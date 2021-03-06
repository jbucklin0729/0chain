package block

import (
	"0chain.net/chaincore/node"
	"0chain.net/chaincore/threshold/bls"
	"0chain.net/core/datastore"
	"0chain.net/core/encryption"
	"0chain.net/core/util"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"sync"

	. "0chain.net/core/logging"
	"go.uber.org/zap"
)

type MagicBlock struct {
	datastore.HashIDField
	mutex                  sync.RWMutex
	PreviousMagicBlockHash datastore.Key       `json:"previous_hash"`
	MagicBlockNumber       int64               `json:"magic_block_number"`
	StartingRound          int64               `json:"starting_round"`
	Miners                 *node.Pool          `json:"miners"`   //this is the pool of miners participating in the blockchain
	Sharders               *node.Pool          `json:"sharders"` //this is the pool of sharders participaing in the blockchain
	ShareOrSigns           *GroupSharesOrSigns `json:"share_or_signs"`
	Mpks                   *Mpks               `json:"mpks"`
	T                      int                 `json:"t"`
	K                      int                 `json:"k"`
	N                      int                 `json:"n"`
}

func NewMagicBlock() *MagicBlock {
	return &MagicBlock{Mpks: NewMpks(), ShareOrSigns: NewGroupSharesOrSigns()}
}

func (mb *MagicBlock) GetShareOrSigns() *GroupSharesOrSigns {
	mb.mutex.RLock()
	defer mb.mutex.RUnlock()
	return mb.ShareOrSigns
}

func (mb *MagicBlock) SetShareOrSigns(gsos *GroupSharesOrSigns) {
	mb.mutex.Lock()
	defer mb.mutex.Unlock()
	mb.ShareOrSigns = gsos
}

func (mb *MagicBlock) Encode() []byte {
	buff, _ := json.Marshal(mb)
	return buff
}

func (mb *MagicBlock) Decode(input []byte) error {
	return json.Unmarshal(input, mb)
}

func (mb *MagicBlock) GetHash() string {
	return util.ToHex(mb.GetHashBytes())
}

func (mb *MagicBlock) GetHashBytes() []byte {
	data := []byte(strconv.FormatInt(mb.MagicBlockNumber, 10))
	data = append(data, []byte(mb.PreviousMagicBlockHash)...)
	data = append(data, []byte(strconv.FormatInt(mb.StartingRound, 10))...)
	var minerKeys, sharderKeys, mpkKeys []string
	// miner info
	minerKeys = mb.Miners.Keys()
	sort.Strings(minerKeys)
	for _, v := range minerKeys {
		data = append(data, []byte(v)...)
	}
	// sharder info
	sharderKeys = mb.Sharders.Keys()
	sort.Strings(sharderKeys)
	for _, v := range sharderKeys {
		data = append(data, []byte(v)...)
	}
	// share info
	shareBytes, _ := hex.DecodeString(mb.GetShareOrSigns().GetHash())
	data = append(data, shareBytes...)
	// mpk info
	for k := range mb.Mpks.Mpks {
		mpkKeys = append(mpkKeys, k)
	}
	sort.Strings(mpkKeys)
	for _, v := range sharderKeys {
		data = append(data, []byte(v)...)
	}
	data = append(data, []byte(strconv.Itoa(mb.T))...)
	data = append(data, []byte(strconv.Itoa(mb.N))...)
	return encryption.RawHash(data)
}

func (mb *MagicBlock) IsActiveNode(id string, round int64) bool {
	if mb == nil || mb.Miners == nil || mb.Sharders == nil {
		return false
	}
	if mb.Miners.HasNode(id) {
		return mb.StartingRound <= round
	}
	return mb.Sharders.HasNode(id) && mb.StartingRound <= round
}

func (mb *MagicBlock) VerifyMinersSignatures(b *Block) bool {
	for _, bvt := range b.GetVerificationTickets() {
		var sender = mb.Miners.GetNode(bvt.VerifierID)
		if sender == nil {
			return false
		}
		if ok, _ := sender.Verify(bvt.Signature, b.Hash); !ok {
			return false
		}
	}
	return true
}

type GroupSharesOrSigns struct {
	mutex  sync.RWMutex
	Shares map[string]*ShareOrSigns `json:"shares"`
}

func NewGroupSharesOrSigns() *GroupSharesOrSigns {
	return &GroupSharesOrSigns{Shares: make(map[string]*ShareOrSigns)}
}

func (gsos *GroupSharesOrSigns) Get(id string) (*ShareOrSigns, bool) {
	gsos.mutex.RLock()
	defer gsos.mutex.RUnlock()
	share, ok := gsos.Shares[id]
	return share, ok
}

func (gsos *GroupSharesOrSigns) GetShares() map[string]*ShareOrSigns {
	gsos.mutex.RLock()
	defer gsos.mutex.RUnlock()
	result := make(map[string]*ShareOrSigns, len(gsos.Shares))
	for k, v := range gsos.Shares {
		result[k] = v
	}
	return result
}

func (gsos *GroupSharesOrSigns) Encode() []byte {
	buff, _ := json.Marshal(gsos)
	return buff
}

func (gsos *GroupSharesOrSigns) Decode(input []byte) error {
	return json.Unmarshal(input, gsos)
}

func (gsos *GroupSharesOrSigns) GetHash() string {
	return util.ToHex(gsos.GetHashBytes())
}

func (gsos *GroupSharesOrSigns) GetHashBytes() []byte {
	var data []byte
	var keys []string
	for k := range gsos.Shares {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		bytes, _ := hex.DecodeString(gsos.Shares[k].Hash())
		data = append(data, bytes...)
	}
	return encryption.RawHash(data)
}

type ShareOrSigns struct {
	ID           string                      `json:"id"`
	ShareOrSigns map[string]*bls.DKGKeyShare `json:"share_or_sign"`
}

func NewShareOrSigns() *ShareOrSigns {
	return &ShareOrSigns{ShareOrSigns: make(map[string]*bls.DKGKeyShare)}
}

func (sos *ShareOrSigns) Hash() string {
	data := sos.ID
	var keys []string
	for k := range sos.ShareOrSigns {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		data += string(sos.ShareOrSigns[k].Encode())
	}
	return encryption.Hash(data)
}

// Validate
func (sos *ShareOrSigns) Validate(mpks *Mpks, publicKeys map[string]string, scheme encryption.SignatureScheme) ([]string, bool) {
	var shares []string
	for key, share := range sos.ShareOrSigns {
		if share.Sign != "" {
			signatureScheme := scheme
			pk, ok := publicKeys[key]
			if !ok {
				return nil, false
			}
			signatureScheme.SetPublicKey(pk)
			sigOK, err := signatureScheme.Verify(share.Sign, share.Message)
			if !sigOK || err != nil {
				Logger.Error("failed to validate share or sings", zap.Any("share", share), zap.Any("message", share.Message), zap.Any("sign", share.Sign))
				return nil, false
			}
		} else {
			var sij bls.Key
			sij.SetHexString(share.Share)
			if !bls.ValidateShare(bls.ConvertStringToMpk(mpks.Mpks[sos.ID].Mpk), sij, bls.ComputeIDdkg(key)) {
				Logger.Error("failed to validate share or sings", zap.Any("share", share), zap.Any("sij.pi", sij.GetPublicKey().GetHexString()))
				return nil, false
			}
			shares = append(shares, key)
		}
	}
	return shares, true
}

func (sos *ShareOrSigns) Encode() []byte {
	buff, _ := json.Marshal(sos)
	return buff
}

func (sos *ShareOrSigns) Decode(input []byte) error {
	return json.Unmarshal(input, sos)
}

func (sos *ShareOrSigns) Clone() *ShareOrSigns {
	clone := &ShareOrSigns{
		ID:           sos.ID,
		ShareOrSigns: make(map[string]*bls.DKGKeyShare, len(sos.ShareOrSigns)),
	}
	for key, dkg := range sos.ShareOrSigns {
		clone.ShareOrSigns[key] = &bls.DKGKeyShare{
			IDField: dkg.IDField,
			Message: dkg.Message,
			Share:   dkg.Share,
			Sign:    dkg.Sign,
		}
	}
	return clone
}

type Mpks struct {
	Mpks map[string]*MPK
}

func NewMpks() *Mpks {
	return &Mpks{Mpks: make(map[string]*MPK)}
}

func (mpks *Mpks) Encode() []byte {
	buff, _ := json.Marshal(mpks)
	return buff
}

func (mpks *Mpks) Decode(input []byte) error {
	err := json.Unmarshal(input, mpks)
	if err != nil {
		return err
	}
	return nil
}

func (mpks *Mpks) GetHash() string {
	return util.ToHex(mpks.GetHashBytes())
}

func (mpks *Mpks) GetHashBytes() []byte {
	return encryption.RawHash(mpks.Encode())
}

func (mpks *Mpks) GetMpkMap() map[bls.PartyID][]bls.PublicKey {
	mpkMap := make(map[bls.PartyID][]bls.PublicKey)
	for k, v := range mpks.Mpks {
		mpkMap[bls.ComputeIDdkg(k)] = bls.ConvertStringToMpk(v.Mpk)
	}
	return mpkMap
}

func (mpks *Mpks) GetMpks() map[string]*MPK {
	result := make(map[string]*MPK, len(mpks.Mpks))
	for k, v := range mpks.Mpks {
		result[k] = v
	}
	return result
}

type MPK struct {
	ID  string
	Mpk []string
}

func (mpk *MPK) Encode() []byte {
	buff, _ := json.Marshal(mpk)
	return buff
}

func (mpk *MPK) Decode(input []byte) error {
	err := json.Unmarshal(input, mpk)
	if err != nil {
		return err
	}
	return nil
}
