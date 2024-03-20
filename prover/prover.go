package prover

/*
#include <stdint.h>
#include <stdlib.h>
#include <stddef.h>

#define FELT_SIZE 32

// Todo: placeholder
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

extern void snosRunnerRun();

// Todo: ???
#cgo vm_debug  LDFLAGS: -L./rust/target/debug   -ljuno_starknet_rs -ldl -lm
#cgo !vm_debug LDFLAGS: -L./rust/target/release -ljuno_starknet_rs -ldl -lm
*/
import "C"
import "github.com/NethermindEth/juno/utils"

type Prover interface {
	snosRunnerRun() // Tod
}

type prover struct {
	log utils.SimpleLogger
}

func New(log utils.SimpleLogger) Prover {
	return &prover{
		log: log,
	}
}
func (p *prover) snosRunnerRun() {
	// Todo: generate relevant data
	// Todo: call the rust function
	// Todo: handle the "returned" data
}
