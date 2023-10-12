package trie

import (
	"bytes"
	"errors"

	"github.com/NethermindEth/juno/core/felt"
)

// A Node represents a node in the [Trie]
type Node struct {
	Value felt.Felt
	Left  *Key
	Right *Key
}

// Hash calculates the hash of a [Node]
func (n *Node) Hash(path *Key, hashFunc hashFunc) felt.Felt {
	if path.Len() == 0 {
		// we have to deference the Value, since the Node can released back
		// to the NodePool and be reused anytime
		return n.Value
	}

	pathFelt := path.Felt()
	// https://docs.starknet.io/documentation/develop/State/starknet-state/
	hash := hashFunc(&n.Value, &pathFelt)
	pathFelt.SetUint64(uint64(path.Len()))
	hash.Add(hash, &pathFelt)
	return *hash
}

func (n *Node) WriteTo(buf *bytes.Buffer) (int64, error) {
	totalBytes := int64(0)

	valueB := n.Value.Bytes()
	wrote, err := buf.Write(valueB[:])
	totalBytes += int64(wrote)
	if err != nil {
		return totalBytes, err
	}

	if n.Left != nil {
		wrote, err := n.Left.WriteTo(buf)
		totalBytes += wrote
		if err != nil {
			return totalBytes, err
		}
		wrote, err = n.Right.WriteTo(buf) // n.Right is non-nil by design
		totalBytes += wrote
		if err != nil {
			return totalBytes, err
		}
	}

	return totalBytes, nil
}

// UnmarshalBinary deserializes a [Node] from a byte array
func (n *Node) UnmarshalBinary(data []byte) error {
	if len(data) < felt.Bytes {
		return errors.New("size of input data is less than felt size")
	}
	n.Value.SetBytes(data[:felt.Bytes])
	data = data[felt.Bytes:]

	if len(data) == 0 {
		n.Left = nil
		n.Right = nil
		return nil
	}

	if n.Left == nil {
		n.Left = new(Key)
		n.Right = new(Key)
	}

	if err := n.Left.UnmarshalBinary(data); err != nil {
		return err
	}
	return n.Right.UnmarshalBinary(data[n.Left.EncodedLen():])
}
