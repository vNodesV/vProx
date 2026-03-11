package status

import (
	"fmt"
	"strings"
)

// bech32Alphabet is the standard bech32 character set (BIP 173).
const bech32Alphabet = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// bech32Encode encodes data bytes with the given human-readable part.
func bech32Encode(hrp string, data []byte) (string, error) {
	conv, err := convertBits(data, 8, 5, true)
	if err != nil {
		return "", err
	}
	checksum := bech32Checksum(hrp, conv)
	combined := append(conv, checksum...)
	var sb strings.Builder
	sb.WriteString(hrp)
	sb.WriteByte('1')
	for _, b := range combined {
		sb.WriteByte(bech32Alphabet[b])
	}
	return sb.String(), nil
}

// convertBits converts a byte slice between bit groupings (e.g. 8-bit → 5-bit).
func convertBits(data []byte, fromBits, toBits uint, pad bool) ([]byte, error) {
	acc := 0
	bits := uint(0)
	maxv := (1 << toBits) - 1
	result := make([]byte, 0, len(data)*int(fromBits)/int(toBits)+1)
	for _, b := range data {
		acc = (acc << fromBits) | int(b)
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			result = append(result, byte((acc>>bits)&maxv))
		}
	}
	if pad {
		if bits > 0 {
			result = append(result, byte((acc<<(toBits-bits))&maxv))
		}
	} else if bits >= fromBits || ((acc<<(toBits-bits))&maxv) != 0 {
		return nil, fmt.Errorf("bech32: invalid bit conversion")
	}
	return result, nil
}

var bech32GenPoly = [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}

func bech32Polymod(values []byte) uint32 {
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>uint(i))&1 != 0 {
				chk ^= bech32GenPoly[i]
			}
		}
	}
	return chk
}

func bech32HRPExpand(hrp string) []byte {
	out := make([]byte, len(hrp)*2+1)
	for i, c := range hrp {
		out[i] = byte(c >> 5)
		out[i+len(hrp)+1] = byte(c & 31)
	}
	return out
}

func bech32Checksum(hrp string, data []byte) []byte {
	values := bech32HRPExpand(hrp)
	values = append(values, data...)
	values = append(values, 0, 0, 0, 0, 0, 0)
	polymod := bech32Polymod(values) ^ 1
	checksum := make([]byte, 6)
	for i := range checksum {
		checksum[i] = byte((polymod >> uint(5*(5-i))) & 31)
	}
	return checksum
}
