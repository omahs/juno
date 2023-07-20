//go:build integration

package junotest

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/NethermindEth/juno/blockchain"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/db"
	"github.com/NethermindEth/juno/db/pebble"
	"github.com/NethermindEth/juno/encoder"
	"github.com/NethermindEth/juno/rpc"
	"github.com/NethermindEth/juno/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sourcegraph/conc/stream"
	"github.com/stretchr/testify/require"
)

type txAndReceiptDBKey struct {
	Number uint64 `json:"number"`
	Index  uint64 `json:"index"`
}

func (t *txAndReceiptDBKey) MarshalBinary() []byte {
	return binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64([]byte{}, t.Number), t.Index)
}

type request struct {
	Version string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      any    `json:"id,omitempty"`
}

func (t *txAndReceiptDBKey) UnmarshalBinary(data []byte) error {
	r := bytes.NewReader(data)
	if err := binary.Read(r, binary.BigEndian, &t.Number); err != nil {
		return err
	}
	return binary.Read(r, binary.BigEndian, &t.Index)
}

type L1HandlerEstimate struct {
	Estimate *rpc.FeeEstimate `json:"estimate"`
}

func TestEstimateMessageFee(t *testing.T) {
	// TODO use command line flags instead. See `go help testflag`
	dbPathEnv := os.Getenv("JUNOTEST_DB_PATH")
	dbPath := filepath.Clean(dbPathEnv)
	networkEnv := os.Getenv("JUNOTEST_NETWORK")
	var network utils.Network
	err := network.Set(networkEnv)
	require.NoError(t, err)

	log, err := utils.NewZapLogger(utils.DEBUG, true)
	require.NoError(t, err)
	pebbleDB, err := pebble.New(dbPath, log)
	require.NoError(t, err)

	txn := pebbleDB.NewTransaction(false)

	it, err := txn.NewIterator()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, it.Close())
	})

	prefix := db.TransactionsByBlockNumberAndIndex.Key()

	results := make(map[uint64]map[uint64]*rpc.FeeEstimate)

	chain := blockchain.New(pebbleDB, network, log)
	handler := rpc.New(chain, nil, network, nil, nil, "", log)

	prefixLength := len(prefix)

	workerStream := stream.New().WithMaxGoroutines(runtime.GOMAXPROCS(0))

	// Get every L1 Handler in the database.
	for it.Seek(prefix); it.Valid(); it.Next() {
		key := it.Key()
		// Stop iterating once we're out of the old bucket.
		if !bytes.Equal(key[:prefixLength], prefix) {
			break
		}

		val, err := it.Value()
		require.NoError(t, err)

		workerStream.Go(func() stream.Callback {
			txKey, estimate := getEstimate(t, handler, log, prefixLength, key, val)
			if txKey == nil {
				return func() {}
			}
			return func() {
				if results[txKey.Number] == nil {
					results[txKey.Number] = make(map[uint64]*rpc.FeeEstimate)
				}
				results[txKey.Number][txKey.Index] = estimate
			}
		})
	}

	workerStream.Wait()

	// Write estimates to file.
	result, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	f, err := os.Create("output.json")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()
	_, err = f.Write(result)
	require.NoError(t, err)
}

func getEstimate(t *testing.T, handler *rpc.Handler, log utils.SimpleLogger, prefixLength int, key, value []byte) (*txAndReceiptDBKey, *rpc.FeeEstimate) {
	txKey := new(txAndReceiptDBKey)

	err := txKey.UnmarshalBinary(key[prefixLength:])
	require.NoError(t, err)

	var tx core.Transaction
	err = encoder.Unmarshal(value, &tx)
	require.NoError(t, err)

	l1Handler, ok := tx.(*core.L1HandlerTransaction)
	if !ok {
		return nil, nil
	}
	log.Infow("L1 handler", "number", txKey.Number, "index", txKey.Index)

	addrBytes := l1Handler.CallData[0].Bytes()

	msgFromL1 := rpc.MsgFromL1{
		From:     common.BytesToAddress(addrBytes[:]),
		To:       l1Handler.ContractAddress,
		Payload:  l1Handler.CallData[1:],
		Selector: l1Handler.EntryPointSelector,
	}

	blockID := rpc.BlockID{
		Number: txKey.Number-1,
	}

	estimate, rpcErr := handler.EstimateMessageFee(msgFromL1, blockID)
	if rpcErr != nil {
		log.Errorw("Failed to estimate message fee", "errCode", rpcErr.Code, "errMsg", rpcErr.Message, "errData", rpcErr.Data)
		return nil, nil
	}

	return txKey, estimate
}
