package core

import (
	"fmt"
	"sort"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
)

type StateUpdate struct {
	BlockHash *felt.Felt
	NewRoot   *felt.Felt
	OldRoot   *felt.Felt
	StateDiff *StateDiff
}

type StateDiff struct {
	StorageDiffs      map[felt.Felt]map[felt.Felt]*felt.Felt // addr -> {key -> value, ...}
	Nonces            map[felt.Felt]*felt.Felt               // addr -> nonce
	DeployedContracts map[felt.Felt]*felt.Felt               // addr -> class hash
	DeclaredV0Classes []*felt.Felt                           // class hashes
	DeclaredV1Classes map[felt.Felt]*felt.Felt               // class hash -> compiled class hash
	ReplacedClasses   map[felt.Felt]*felt.Felt               // addr -> class hash
}

type StateUpdateJSON struct {
	BlockHash *felt.Felt     `json:"block_hash"`
	NewRoot   *felt.Felt     `json:"new_root"`
	OldRoot   *felt.Felt     `json:"old_root"`
	StateDiff *StateDiffJSON `json:"state_diff"`
}

type DeployedContract struct {
	Address   *felt.Felt `json:"address"`
	ClassHash *felt.Felt `json:"class_hash"`
}

type DeclaredV1Classes struct {
	ClassHash         *felt.Felt `json:"class_hash"`
	CompiledClassHash *felt.Felt `json:"compiled_class_hash"`
}

type StateDiffKeyValue struct {
	Key   *felt.Felt `json:"key"`
	Value *felt.Felt `json:"value"`
}

type StateDiffJSON struct {
	StorageDiffs      map[string][]StateDiffKeyValue `json:"storage_diffs"`
	Nonces            map[string]*felt.Felt          `json:"nonces"`
	DeployedContracts []DeployedContract             `json:"deployed_contracts"`
	DeclaredV0Classes []*felt.Felt                   `json:"old_declared_contracts"`
	DeclaredV1Classes []DeclaredV1Classes            `json:"declared_classes"`
	ReplacedClasses   []DeployedContract             `json:"replaced_classes"`
}

func StateUpdateAdapter(stateUpdateJSON StateUpdateJSON) StateUpdate {
	storageDiffs := make(map[felt.Felt]map[felt.Felt]*felt.Felt)
	for addr, keyValueArr := range stateUpdateJSON.StateDiff.StorageDiffs {
		addrFelt := felt.Felt{}
		err := addrFelt.UnmarshalJSON([]byte(addr))
		if err != nil {
			return StateUpdate{}
		}
		storageDiffs[addrFelt] = make(map[felt.Felt]*felt.Felt)
		for _, keyValue := range keyValueArr {
			storageDiffs[addrFelt][*keyValue.Key] = keyValue.Value
		}
	}

	deployedContracts := make(map[felt.Felt]*felt.Felt)
	for _, contract := range stateUpdateJSON.StateDiff.DeployedContracts {
		deployedContracts[*contract.Address] = contract.ClassHash
	}

	deployedV1Class := make(map[felt.Felt]*felt.Felt)
	for _, classV1 := range stateUpdateJSON.StateDiff.DeclaredV1Classes {
		deployedV1Class[*classV1.ClassHash] = classV1.CompiledClassHash
	}

	replacedClasses := make(map[felt.Felt]*felt.Felt)
	for _, replaced := range stateUpdateJSON.StateDiff.ReplacedClasses {
		replacedClasses[*replaced.Address] = replaced.ClassHash
	}

	stateDiff := StateDiff{
		StorageDiffs:      storageDiffs,
		Nonces:            convertToFeltMap(stateUpdateJSON.StateDiff.Nonces),
		DeployedContracts: deployedContracts,
		DeclaredV0Classes: stateUpdateJSON.StateDiff.DeclaredV0Classes,
		DeclaredV1Classes: deployedV1Class,
		ReplacedClasses:   replacedClasses,
	}

	return StateUpdate{
		BlockHash: stateUpdateJSON.BlockHash,
		NewRoot:   stateUpdateJSON.NewRoot,
		OldRoot:   stateUpdateJSON.OldRoot,
		StateDiff: &stateDiff,
	}
}

func convertToFeltMap(input map[string]*felt.Felt) map[felt.Felt]*felt.Felt {
	output := make(map[felt.Felt]*felt.Felt)
	for k, v := range input {
		keyFelt := felt.Felt{}
		err := keyFelt.UnmarshalJSON([]byte(k))
		if err != nil {
			output[keyFelt] = &keyFelt
		}
		output[keyFelt] = v
	}
	return output
}

func EmptyStateDiff() *StateDiff {
	return &StateDiff{
		StorageDiffs:      make(map[felt.Felt]map[felt.Felt]*felt.Felt),
		Nonces:            make(map[felt.Felt]*felt.Felt),
		DeployedContracts: make(map[felt.Felt]*felt.Felt),
		DeclaredV0Classes: make([]*felt.Felt, 0),
		DeclaredV1Classes: make(map[felt.Felt]*felt.Felt),
		ReplacedClasses:   make(map[felt.Felt]*felt.Felt),
	}
}

func (d *StateDiff) Commitment() *felt.Felt {
	version := felt.Zero
	var tmpFelt felt.Felt

	/*
		hash_of_deployed_contracts=hash([number_of_deployed_contracts, address_1, class_hash_1,
			address_2, class_hash_2, ...])
	*/
	var hashOfDeployedContracts crypto.PoseidonDigest
	deployedReplacedAddresses := make([]felt.Felt, 0, len(d.DeployedContracts)+len(d.ReplacedClasses))
	for addr := range d.DeployedContracts {
		deployedReplacedAddresses = append(deployedReplacedAddresses, addr)
	}
	for addr := range d.ReplacedClasses {
		deployedReplacedAddresses = append(deployedReplacedAddresses, addr)
	}
	hashOfDeployedContracts.Update(tmpFelt.SetUint64(uint64(len(deployedReplacedAddresses))))
	sort.Slice(deployedReplacedAddresses, func(i, j int) bool {
		switch deployedReplacedAddresses[i].Cmp(&deployedReplacedAddresses[j]) {
		case -1:
			return true
		case 1:
			return false
		default:
			// The sequencer guarantees that a contract cannot be:
			// - deployed twice,
			// - deployed and have its class replaced in the same state diff, or
			// - have its class replaced multiple times in the same state diff.
			panic(fmt.Sprintf("address appears twice in deployed and replaced addresses: %s", &deployedReplacedAddresses[i]))
		}
	})
	for idx := range deployedReplacedAddresses {
		addr := deployedReplacedAddresses[idx]
		classHash, ok := d.DeployedContracts[addr]
		if !ok {
			classHash = d.ReplacedClasses[addr]
		}
		hashOfDeployedContracts.Update(&addr, classHash)
	}

	/*
		hash_of_declared_classes = hash([number_of_declared_classes, class_hash_1, compiled_class_hash_1,
			class_hash_2, compiled_class_hash_2, ...])
	*/
	var hashOfDeclaredClasses crypto.PoseidonDigest
	hashOfDeclaredClasses.Update(tmpFelt.SetUint64(uint64(len(d.DeclaredV1Classes))))
	declaredV1ClassHashes := sortedFeltKeys(d.DeclaredV1Classes)
	for idx := range declaredV1ClassHashes {
		classHash := declaredV1ClassHashes[idx]
		hashOfDeclaredClasses.Update(&classHash, d.DeclaredV1Classes[classHash])
	}

	/*
		hash_of_old_declared_classes = hash([number_of_old_declared_classes, class_hash_1, class_hash_2, ...])
	*/
	var hashOfOldDeclaredClasses crypto.PoseidonDigest
	hashOfOldDeclaredClasses.Update(tmpFelt.SetUint64(uint64(len(d.DeclaredV0Classes))))
	sort.Slice(d.DeclaredV0Classes, func(i, j int) bool {
		return d.DeclaredV0Classes[i].Cmp(d.DeclaredV0Classes[j]) == -1
	})
	hashOfOldDeclaredClasses.Update(d.DeclaredV0Classes...)

	/*
		flattened_storage_diffs = [number_of_updated_contracts, contract_address_1, number_of_updates_in_contract,
			key_1, value_1, key_2, value_2, ..., contract_address_2, number_of_updates_in_contract, ...]
		flattened_nonces = [number_of_updated_contracts, address_1, nonce_1, address_2, nonce_2, ...]
		hash_of_storage_domain_state_diff = hash([*flattened_storage_diffs, *flattened_nonces])
	*/
	daModeL1 := 0
	hashOfStorageDomains := make([]crypto.PoseidonDigest, 1)

	sortedStorageDiffAddrs := sortedFeltKeys(d.StorageDiffs)
	hashOfStorageDomains[daModeL1].Update(tmpFelt.SetUint64(uint64(len(sortedStorageDiffAddrs))))
	for idx, addr := range sortedStorageDiffAddrs {
		hashOfStorageDomains[daModeL1].Update(&sortedStorageDiffAddrs[idx])
		diffKeys := sortedFeltKeys(d.StorageDiffs[sortedStorageDiffAddrs[idx]])

		hashOfStorageDomains[daModeL1].Update(tmpFelt.SetUint64(uint64(len(diffKeys))))
		for idx := range diffKeys {
			key := diffKeys[idx]
			hashOfStorageDomains[daModeL1].Update(&key, d.StorageDiffs[addr][key])
		}
	}

	sortedNonceKeys := sortedFeltKeys(d.Nonces)
	hashOfStorageDomains[daModeL1].Update(tmpFelt.SetUint64(uint64(len(sortedNonceKeys))))
	for idx := range sortedNonceKeys {
		hashOfStorageDomains[daModeL1].Update(&sortedNonceKeys[idx], d.Nonces[sortedNonceKeys[idx]])
	}

	/*
		flattened_total_state_diff = hash([state_diff_version,
			hash_of_deployed_contracts, hash_of_declared_classes,
			hash_of_old_declared_classes, number_of_DA_modes,
			DA_mode_0, hash_of_storage_domain_state_diff_0, DA_mode_1, hash_of_storage_domain_state_diff_1, …])
	*/
	var commitmentDigest crypto.PoseidonDigest
	commitmentDigest.Update(&version, hashOfDeployedContracts.Finish(), hashOfDeclaredClasses.Finish(), hashOfOldDeclaredClasses.Finish())
	commitmentDigest.Update(tmpFelt.SetUint64(uint64(len(hashOfStorageDomains))))
	for idx := range hashOfStorageDomains {
		commitmentDigest.Update(tmpFelt.SetUint64(uint64(idx)), hashOfStorageDomains[idx].Finish())
	}
	return commitmentDigest.Finish()
}

func sortedFeltKeys[V any](m map[felt.Felt]V) []felt.Felt {
	keys := make([]felt.Felt, 0, len(m))
	for addr := range m {
		keys = append(keys, addr)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Cmp(&keys[j]) == -1
	})

	return keys
}
