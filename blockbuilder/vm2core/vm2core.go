package vm2core

import (
	"encoding/json"
	"fmt"

	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/felt"
)

type vmStateDiff struct {
	StorageDiff []struct {
		Address        *felt.Felt `json:"address"`
		StorageEntries []struct {
			Key   *felt.Felt `json:"key"`
			Value *felt.Felt `json:"value"`
		} `json:"storage_entries"`
	} `json:"storage_diffs"`
	Nonces []struct {
		ContractAddress *felt.Felt `json:"contract_address"`
		Nonce           *felt.Felt `json:"nonce"`
	} `json:"nonces"`
	DeployedContracts []struct {
		Address   *felt.Felt `json:"address"`
		ClassHash *felt.Felt `json:"class_hash"`
	} `json:"deployed_contracts"`
	DeprecatedDeclaredClasses []*felt.Felt `json:"deprecated_declared_classes"`
	DeclaredClasses           []struct {
		ClassHash         *felt.Felt `json:"class_hash"`
		CompiledClassHash *felt.Felt `json:"compiled_class_hash"`
	} `json:"declared_classes"`
	ReplacedClasses []struct {
		ContractAddress *felt.Felt `json:"contract_address"`
		ClassHash       *felt.Felt `json:"class_hash"`
	} `json:"replaced_classes"`
}

type vmTrace struct {
	StateDiff *vmStateDiff `json:"state_diff"`
}

func TraceToStateDiff(traceJSON json.RawMessage) (*core.StateDiff, error) {
	trace := new(vmTrace)
	if err := json.Unmarshal(traceJSON, &trace); err != nil {
		return nil, fmt.Errorf("unmarshal json trace: %v", err)
	}
	vmDiff := trace.StateDiff

	storageDiffs := make(map[felt.Felt][]core.StorageDiff)
	for _, diff := range vmDiff.StorageDiff {
		entries := make([]core.StorageDiff, len(diff.StorageEntries))
		for i, entry := range diff.StorageEntries {
			entries[i] = core.StorageDiff{
				Key:   entry.Key,
				Value: entry.Value,
			}
		}
		storageDiffs[*diff.Address] = entries
	}

	nonces := make(map[felt.Felt]*felt.Felt)
	for _, addrNoncePair := range vmDiff.Nonces {
		nonces[*addrNoncePair.ContractAddress] = addrNoncePair.Nonce
	}

	deployedContracts := make([]core.AddressClassHashPair, len(vmDiff.DeployedContracts))
	for i, deployedContract := range vmDiff.DeployedContracts {
		deployedContracts[i] = core.AddressClassHashPair{
			Address:   deployedContract.Address,
			ClassHash: deployedContract.ClassHash,
		}
	}

	declaredV1Classes := make([]core.DeclaredV1Class, len(vmDiff.DeclaredClasses))
	for i, declaredClass := range vmDiff.DeclaredClasses {
		declaredV1Classes[i] = core.DeclaredV1Class{
			ClassHash:         declaredClass.ClassHash,
			CompiledClassHash: declaredClass.CompiledClassHash,
		}
	}

	replacedClasses := make([]core.AddressClassHashPair, len(vmDiff.ReplacedClasses))
	for i, replacedClass := range vmDiff.ReplacedClasses {
		replacedClasses[i] = core.AddressClassHashPair{
			Address:   replacedClass.ClassHash,
			ClassHash: replacedClass.ClassHash,
		}
	}

	return &core.StateDiff{
		StorageDiffs:      storageDiffs,
		Nonces:            nonces,
		DeployedContracts: deployedContracts,
		DeclaredV0Classes: vmDiff.DeprecatedDeclaredClasses,
		DeclaredV1Classes: declaredV1Classes,
		ReplacedClasses:   replacedClasses,
	}, nil
}
