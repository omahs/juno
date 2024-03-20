package prover

/*
#include <stdint.h>
#include <stdlib.h>
#include <stddef.h>

#define FELT_SIZE 32

typedef struct BlockInfo {
	unsigned long long block_number;
	unsigned long long block_timestamp;
	unsigned char sequencer_address[FELT_SIZE];
	unsigned char gas_price_wei[FELT_SIZE];
	unsigned char gas_price_fri[FELT_SIZE];
	char* version;
	unsigned char block_hash_to_be_revealed[FELT_SIZE];
	unsigned char data_gas_price_wei[FELT_SIZE];
	unsigned char data_gas_price_fri[FELT_SIZE];
	unsigned char use_blob_data;
} BlockInfo;

extern void snosRunnerRun(BlockInfo* block_info_ptr, uintptr_t readerHandle, char* chain_id);

// Todo: ???
#cgo vm_debug  LDFLAGS: -L./rust/target/debug   -ljuno_starknet_rs -ldl -lm
#cgo !vm_debug LDFLAGS: -L./rust/target/release -ljuno_starknet_rs -ldl -lm
*/
import "C"
import (
	"encoding/json"
	"runtime/cgo"
	"unsafe"

	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
)

type BlockInfo struct {
	Header                *core.Header
	BlockHashToBeRevealed *felt.Felt
}

type Prover interface {
	snosRunnerRun(blockInfo *BlockInfo, useBlobData bool, network *utils.Network, state core.StateReader) error // Todo
}

type prover struct {
	log utils.SimpleLogger
}

func New(log utils.SimpleLogger) Prover {
	return &prover{
		log: log,
	}
}
func (p *prover) snosRunnerRun(blockInfo *BlockInfo, useBlobData bool, network *utils.Network, state core.StateReader) error {
	// Todo: generate relevant data (block data and transactions in json form)
	// Todo: handle the "returned" data
	context := &callContext{
		state: state,
		log:   p.log,
	}
	handle := cgo.NewHandle(context)
	defer handle.Delete()
	cBlockInfo := makeCBlockInfo(blockInfo, useBlobData)
	chainID := C.CString(network.L2ChainID)
	C.snosRunnerRun(&cBlockInfo, C.uintptr_t(handle), chainID)
	C.free(unsafe.Pointer(chainID))
	C.free(unsafe.Pointer(cBlockInfo.version))
	return nil
}

type callContext struct {
	// state that the call is running on
	state core.StateReader
	log   utils.SimpleLogger
	// err field to be possibly populated in case of an error in execution
	err string
	// index of the transaction that generated err
	errTxnIndex int64
	// response from the executed Cairo function
	response []*felt.Felt
	// fee amount taken per transaction during VM execution
	actualFees      []*felt.Felt
	traces          []json.RawMessage
	dataGasConsumed []*felt.Felt
}

func makeCBlockInfo(blockInfo *BlockInfo, useBlobData bool) C.BlockInfo {
	var cBlockInfo C.BlockInfo

	cBlockInfo.block_number = C.ulonglong(blockInfo.Header.Number)
	cBlockInfo.block_timestamp = C.ulonglong(blockInfo.Header.Timestamp)
	copyFeltIntoCArray(blockInfo.Header.SequencerAddress, &cBlockInfo.sequencer_address[0])
	copyFeltIntoCArray(blockInfo.Header.GasPrice, &cBlockInfo.gas_price_wei[0])
	copyFeltIntoCArray(blockInfo.Header.GasPriceSTRK, &cBlockInfo.gas_price_fri[0])
	cBlockInfo.version = cstring([]byte(blockInfo.Header.ProtocolVersion))
	copyFeltIntoCArray(blockInfo.BlockHashToBeRevealed, &cBlockInfo.block_hash_to_be_revealed[0])
	if blockInfo.Header.L1DAMode == core.Blob {
		copyFeltIntoCArray(blockInfo.Header.L1DataGasPrice.PriceInWei, &cBlockInfo.data_gas_price_wei[0])
		copyFeltIntoCArray(blockInfo.Header.L1DataGasPrice.PriceInFri, &cBlockInfo.data_gas_price_fri[0])
		if useBlobData {
			cBlockInfo.use_blob_data = 1
		} else {
			cBlockInfo.use_blob_data = 0
		}
	}
	return cBlockInfo
}

func copyFeltIntoCArray(f *felt.Felt, cArrPtr *C.uchar) {
	if f == nil {
		return
	}

	feltBytes := f.Bytes()
	cArr := unsafe.Slice(cArrPtr, len(feltBytes))
	for index := range feltBytes {
		cArr[index] = C.uchar(feltBytes[index])
	}
}
