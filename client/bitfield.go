package main

import "fmt"

type Bitfield []byte

// all bits are initially set to 0 (meaning we have no pieces).
func NewBitfield(numPieces int) Bitfield {
	numBytes := (numPieces + 7) / 8
	return make(Bitfield, numBytes)
}

// checks if the bit for a given piece index is set.
func (bf Bitfield) Has(index int) bool {
	byteIndex := index / 8
	bitIndex := index % 8

	if byteIndex < 0 || byteIndex >= len(bf) {
		return false
	}

	mask := byte(1 << (7 - bitIndex))
	return (bf[byteIndex] & mask) != 0
}

func (bf Bitfield) Set(index int) error {
	byteIndex := index / 8
	bitIndex := index % 8

	if byteIndex < 0 || byteIndex >= len(bf) {
		return fmt.Errorf("bitfield index out of bounds")
	}

	mask := byte(1 << (7 - bitIndex))
	bf[byteIndex] = bf[byteIndex] | mask
	return nil
}
