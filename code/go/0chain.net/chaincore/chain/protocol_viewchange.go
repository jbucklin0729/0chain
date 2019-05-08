package chain

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime/pprof"
	"sync"

	"0chain.net/chaincore/config"
	"0chain.net/chaincore/node"
	"0chain.net/core/common"
	. "0chain.net/core/logging"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

//MagicBlock to create and track active sets
type MagicBlock struct {
	MagicBlockNumber   int64 `json:"magic_block_number,omitempty"`
	StartingRound      int64 `json:"starting_round,omitempty"`
	EstimatedLastRound int64 `json:"estimated_last_round,omitempty"`
	ActiveSetMax       int

	/*Miners - this is the pool of miners participating in the blockchain */
	ActiveSetMiners *node.Pool `json:"-"`

	/*Sharders - this is the pool of sharders participaing in the blockchain*/
	ActiveSetSharders *node.Pool `json:"-"`

	/*Miners - this is the pool of all miners */
	AllMiners *node.Pool `json:"-"`

	/*Sharders - this is the pool of all sharders */
	AllSharders *node.Pool `json:"-"`

	/*DKGSetMiners -- this is the pool of all Miners in the DKG process */
	DKGSetMiners *node.Pool `json:"-"`

	VcVrfShare *VCVRFShare

	RandomSeed int64 `json:"random_seed,omitempty"`
	minerPerm  []int

	recVcVrfSharesMap map[string]*VCVRFShare
	ActiveSetMaxSize  int
	ActiveSetMinSize  int
	Mutex             sync.RWMutex
}

//SetupMagicBlock create and setup magicblock object
func SetupMagicBlock(startingRound int64, life int64, activeSetMaxSize int, activeSetMinSize int) *MagicBlock {
	mb := &MagicBlock{}
	mb.StartingRound = startingRound
	mb.EstimatedLastRound = mb.StartingRound + life
	mb.ActiveSetMaxSize = activeSetMaxSize
	mb.ActiveSetMinSize = activeSetMinSize
	Logger.Info("Created magic block", zap.Int64("Starting_round", mb.StartingRound), zap.Int64("ending_round", mb.EstimatedLastRound))
	return mb
}

/*ReadNodePools - read the node pools from configuration */
func (mb *MagicBlock) ReadNodePools(configFile string) error {
	nodeConfig := config.ReadConfig(configFile)
	config := nodeConfig.Get("miners")
	if miners, ok := config.([]interface{}); ok {
		if mb.AllMiners == nil {
			//Reading from config file, the node pools need to be initialized
			mb.AllMiners = node.NewPool(node.NodeTypeMiner)

			mb.AllMiners.AddNodes(miners)
			mb.AllMiners.ComputeProperties()

		}

	}
	config = nodeConfig.Get("sharders")
	if sharders, ok := config.([]interface{}); ok {
		if mb.AllSharders == nil {
			//Reading from config file, the node pools need to be initialized
			mb.AllSharders = node.NewPool(node.NodeTypeSharder)
			mb.ActiveSetSharders = node.NewPool(node.NodeTypeSharder)
			mb.AllSharders.AddNodes(sharders)
			mb.AllSharders.ComputeProperties()
			mb.ActiveSetSharders.AddNodes(sharders)
			mb.ActiveSetSharders.ComputeProperties()
		}

	}

	if mb.AllMiners == nil || mb.AllSharders == nil {
		err := common.NewError("configfile_read_err", "Either sharders or miners or both are not found in "+configFile)
		Logger.Info(err.Error())
		return err
	}
	Logger.Info("Added miners", zap.Int("all_miners", len(mb.AllMiners.Nodes)),
		zap.Int("all_sharders", len(mb.AllSharders.Nodes)),
		zap.Int("active_sharders", len(mb.ActiveSetSharders.Nodes)))

	//ToDo: NeedsFix. We need this because Sharders need this right after reading the pool. Fix it.
	mb.GetComputedDKGSet()
	return nil
}

//GetAllMiners gets all miners node pool
func (mb *MagicBlock) GetAllMiners() *node.Pool {
	return mb.AllMiners
}

//GetActiveSetMiners gets all miners in ActiveSet
func (mb *MagicBlock) GetActiveSetMiners() *node.Pool {
	return mb.ActiveSetMiners
}

//GetDkgSetMiners gets all miners participating in DKG
func (mb *MagicBlock) GetDkgSetMiners() *node.Pool {
	return mb.DKGSetMiners
}

//GetAllSharders Gets all sharders in the pool
func (mb *MagicBlock) GetAllSharders() *node.Pool {
	return mb.AllSharders
}

//GetActiveSetSharders gets all sharders in the active set
func (mb *MagicBlock) GetActiveSetSharders() *node.Pool {
	return mb.ActiveSetSharders
}

// GetComputedDKGSet select and provide miners set for DKG based on the rules
func (mb *MagicBlock) GetComputedDKGSet() (*node.Pool, *common.Error) {
	if mb.DKGSetMiners != nil {
		return mb.DKGSetMiners, nil
	}
	mb.DKGSetMiners = node.NewPool(node.NodeTypeMiner)
	miners, err := mb.getDKGSetAfterRules(mb.GetAllMiners())

	if err != nil || miners == nil {
		return miners, err
	}
	mb.DKGSetMiners = miners
	mb.DKGSetMiners.ComputeProperties()
	Logger.Info("returning computed dkg set miners", zap.Int("dkgset_num", mb.DKGSetMiners.Size()))
	return mb.DKGSetMiners, nil
}

func (mb *MagicBlock) getDKGSetAfterRules(allMiners *node.Pool) (*node.Pool, *common.Error) {
	sc := GetServerChain()

	/*
	   Rule#1: if allMiners size is less than the active set required size, you cannopt proceed
	*/
	if allMiners.Size() < sc.ActiveSetMinerMin {
		return nil, common.NewError("too_few_miners", fmt.Sprintf("Need: %v, Have %v", sc.ActiveSetMinerMin, allMiners.Size()))
	}

	var currActiveSetSize int
	if mb.ActiveSetMiners != nil {
		currActiveSetSize = mb.ActiveSetMiners.Size()
	}

	/*
	  Rule#2: DKGSet size cannot be more than increment size of the current active set size;
	  if starting, assume all miners are eligible
	*/
	var dkgSetSize int
	if currActiveSetSize > 0 {
		dkgSetSize = int(math.Ceil((float64(sc.DkgSetMinerIncMax) / 100) * float64(currActiveSetSize)))
	} else {
		dkgSetSize = allMiners.Size()
	}
	if allMiners.Size() > dkgSetSize {
		Logger.Error("Too many miners Need to use stake logic", zap.Int("need", dkgSetSize), zap.Int("have", allMiners.Size()))
	}
	dkgMiners := node.NewPool(node.NodeTypeMiner)

	for _, miner := range allMiners.Nodes {
		dkgMiners.AddNode(miner)
	}

	return dkgMiners, nil
}

// IsMbReadyForDKG are the miners in DKGSet ready for DKG
func (mb *MagicBlock) IsMbReadyForDKG() bool {
	active := mb.DKGSetMiners.GetActiveCount()
	return active >= mb.DKGSetMiners.Size()
}

// ComputeActiveSetMinersForSharder Temp API for Sharders to start with assumption that all genesys miners are active
func (mb *MagicBlock) ComputeActiveSetMinersForSharder() {
	mb.ActiveSetMiners = node.NewPool(node.NodeTypeMiner)
	//This needs more logic. Simplistic approach of all DKGSet moves to ActiveSet for now
	for _, n := range mb.DKGSetMiners.Nodes {
		mb.ActiveSetMiners.AddNode(n)
	}
	mb.ActiveSetMiners.ComputeProperties()
}

// DKGDone Tell magic block that DKG + vcvrfs is done.
func (mb *MagicBlock) DKGDone(randomSeed int64) {

	mb.RandomSeed = randomSeed
	mb.ComputeMinerRanks(mb.DKGSetMiners)
	rankedMiners := mb.GetMinersByRank(mb.DKGSetMiners)

	Logger.Info("Done computing miner ranks", zap.Int("len_of_miners", len(rankedMiners)))
	mb.ActiveSetMiners = node.NewPool(node.NodeTypeMiner)

	/*
	i := 0
	for _, n := range rankedMiners {
		mb.ActiveSetMiners.AddNode(n)
		if i < mb.ActiveSetMaxSize {
			break
		} else {
			i++
		}
	}
	*/

	for _, n := range mb.DKGSetMiners.Nodes {
			mb.ActiveSetMiners.AddNode(n)
		}
	mb.ActiveSetMiners.ComputeProperties()
}

// AddToVcVrfSharesMap collect vrf shares for VC
func (mb *MagicBlock) AddToVcVrfSharesMap(nodeID string, share *VCVRFShare) bool {
	mb.Mutex.Lock()
	defer mb.Mutex.Unlock()
	dkgSet := mb.GetDkgSetMiners()

	//ToDo: Check if the nodeId is in dkgSet
	if mb.recVcVrfSharesMap == nil {

		mb.recVcVrfSharesMap = make(map[string]*VCVRFShare, len(dkgSet.Nodes))
	}
	if _, ok := mb.recVcVrfSharesMap[nodeID]; ok {
		Logger.Info("Ignoring VcVRF Share recived again from node : ", zap.String("Node_Id", nodeID))
		return false
	}

	mb.recVcVrfSharesMap[nodeID] = share
	return true
}

func (mb *MagicBlock) getVcVrfConsensus() int {
	thresholdByCount := viper.GetInt("server_chain.block.consensus.threshold_by_count")
	return int(math.Ceil((float64(thresholdByCount) / 100) * float64(mb.GetDkgSetMiners().Size())))

}

// IsVcVrfConsensusReached --checks if there are enough VcVrf shares
func (mb *MagicBlock) IsVcVrfConsensusReached() bool {
	return len(mb.recVcVrfSharesMap) >= mb.getVcVrfConsensus()
}

// GetVcVRFShareInfo -- break down VcVRF shares to get the seed
func (mb *MagicBlock) GetVcVRFShareInfo() ([]string, []string) {
	recSig := make([]string, 0)
	recFrom := make([]string, 0)
	mb.Mutex.Lock()
	defer mb.Mutex.Unlock()

	for nodeID, share := range mb.recVcVrfSharesMap {
		recSig = append(recSig, share.Share)
		recFrom = append(recFrom, nodeID)
	}

	return recSig, recFrom
}

/*ComputeMinerRanks - Compute random order of n elements given the random seed of the round */
func (mb *MagicBlock) ComputeMinerRanks(miners *node.Pool) {
	mb.minerPerm = rand.New(rand.NewSource(mb.RandomSeed)).Perm(miners.Size())
}

func (mb *MagicBlock) IsMinerInActiveSet(miner *node.Node) bool {
	return mb.GetMinerRank(miner) <= mb.ActiveSetMaxSize
}

/*GetMinerRank - get the rank of element at the elementIdx position based on the permutation of the round */
func (mb *MagicBlock) GetMinerRank(miner *node.Node) int {
	mb.Mutex.RLock()
	defer mb.Mutex.RUnlock()
	if mb.minerPerm == nil {
		pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
		Logger.DPanic(fmt.Sprintf("miner ranks not computed yet: %v", mb.GetMagicBlockNumber()))
	}
	return mb.minerPerm[miner.SetIndex]
}

/*GetMinersByRank - get the ranks of the miners */
func (mb *MagicBlock) GetMinersByRank(miners *node.Pool) []*node.Node {
	mb.Mutex.RLock()
	defer mb.Mutex.RUnlock()
	nodes := miners.Nodes
	rminers := make([]*node.Node, len(nodes))
	for _, nd := range nodes {
		rminers[mb.minerPerm[nd.SetIndex]] = nd
	}
	return rminers
}

// GetMagicBlockNumber handy API to get the magic block number
func (mb *MagicBlock) GetMagicBlockNumber() int64 {
	return mb.MagicBlockNumber
}
