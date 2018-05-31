package client

import (
	"context"
	"fmt"
	"testing"
	"time"

	"0chain.net/common"
	"0chain.net/datastore"
	"0chain.net/encryption"
	"0chain.net/memorystore"
)

func TestClientChunkSave(t *testing.T) {
	common.SetupRootContext(context.Background())
	SetupEntity()
	fmt.Printf("time : %v\n", time.Now().UnixNano()/int64(time.Millisecond))
	start := time.Now()
	fmt.Printf("Testing at %v\n", start)
	numWorkers := 10000
	done := make(chan bool, 100)
	for i := 1; i <= numWorkers; i++ {
		publicKey, privateKey := encryption.GenerateKeys()
		if privateKey == "" {
			fmt.Println("Error genreating keys")
			continue
		}
		go postClient(publicKey, done)
	}
	for count := 0; true; {
		<-done
		count++
		if count == numWorkers {
			break
		}
	}
	fmt.Printf("Elapsed time: %v\n", time.Since(start))
}

func postClient(publicKey string, done chan<- bool) {
	entity := Provider()
	client, ok := entity.(*Client)
	if !ok {
		fmt.Printf("it's not ok!\n")
	}
	client.PublicKey = publicKey
	client.SetKey(datastore.ToKey(encryption.Hash(client.PublicKey)))

	ctx := memorystore.WithAsyncChannel(context.Background(), ClientEntityChannel)
	_, err := PutClient(ctx, entity)
	if err != nil {
		fmt.Printf("error for %v : %v\n", publicKey, err)
	}
	done <- true
}
