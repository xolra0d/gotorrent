package benserde

import (
	"fmt"
	"io"
)

type BencodeValueType int

const (
	ValueInt BencodeValueType = iota
	ValueString
	ValueArray
	ValueDict
)

type BencodeValue struct {
	vType BencodeValueType
	value any
}

func BencodeDict(dict map[string]BencodeValue) BencodeValue {
	return BencodeValue{
		vType: ValueDict,
		value: dict,
	}
}

func BencodeInt(val int) BencodeValue {
	return BencodeValue{
		vType: ValueInt,
		value: val,
	}
}

func (val BencodeValue) String() string {
	switch val.vType {
	case ValueInt:
		return fmt.Sprintf("Int(%d)", val.value)
	case ValueString:
		return fmt.Sprintf("String(%q)", val.value)
	case ValueArray:
		return fmt.Sprintf("Array(%v)", val.value)
	case ValueDict:
		return fmt.Sprintf("Dict(%v)", val.value)
	default:
		return fmt.Sprintf("Unknown(%v)", val.value)
	}
}

func (val BencodeValue) AsDict() (map[string]BencodeValue, error) {
	if m, ok := val.value.(map[string]BencodeValue); ok && val.vType == ValueDict {
		return m, nil
	}

	return map[string]BencodeValue{}, nil
}

func (val BencodeValue) AsArray() ([]BencodeValue, error) {
	if m, ok := val.value.([]BencodeValue); ok && val.vType == ValueArray {
		return m, nil
	}

	return []BencodeValue{}, nil
}

func (val BencodeValue) AsInt() (int, error) {
	if m, ok := val.value.(int); ok {
		return m, nil
	}

	return 0, nil
}

func (val BencodeValue) AsString() (string, error) {
	if m, ok := val.value.(string); ok {
		return m, nil
	}

	return "", nil
}

type UnexpectedByte byte

func (err UnexpectedByte) Error() string {
	return fmt.Sprintf("Unexpected char: %q", byte(err))
}

func DecodeBencode(encoded []byte) (int, BencodeValue, error) {
	start, val, err := decodeInternal(encoded, 0)
	return start, val, err
}

func decodeInternal(chunk []byte, start int) (int, BencodeValue, error) {
	if len(chunk) <= start {
		return start, BencodeValue{}, io.EOF
	}
	if chunk[start] >= '1' && chunk[start] <= '9' {
		return decodeString(chunk, start)
	} else if chunk[start] == 'i' {
		return decodeInt(chunk, start)
	} else if chunk[start] == 'l' {
		return decodeArray(chunk, start)
	} else if chunk[start] == 'd' {
		return decodeDict(chunk, start)
	}

	return start, BencodeValue{}, UnexpectedByte(chunk[start])
}

func decodeInt(chunk []byte, start int) (int, BencodeValue, error) {
	start += 1 // ignore `i`
	if chunk[start] == '0' {
		// leading zero is not allowed
		if chunk[start+1] != 'e' {
			// `0` is encoded as `i0e`. otherwise it's error
			return start + 1, BencodeValue{}, UnexpectedByte(chunk[start+1])
		}
		return start + 2, BencodeValue{ValueInt, 0}, nil
	}

	neg := false
	if chunk[start] == '-' {
		neg = true
		start += 1 // ignore '-'
	}

	result := 0

	for i := start; i < len(chunk); i++ {
		if chunk[i] >= '0' && chunk[i] <= '9' {
			result += int(chunk[i] - '0')
		} else if chunk[i] == 'e' {
			if neg {
				result = -result
			}
			return i + 1, BencodeValue{ValueInt, result}, nil
		} else {
			return i, BencodeValue{}, UnexpectedByte(chunk[i])
		}
	}
	return len(chunk), BencodeValue{}, io.ErrUnexpectedEOF
}

// we expect first to be num, so `:` is not possible
func decodeString(chunk []byte, start int) (int, BencodeValue, error) {

	if chunk[start] == '0' {
		if chunk[start+1] != ':' {
			return start + 1, BencodeValue{}, UnexpectedByte(chunk[start+1])
		}
		return start + 2, BencodeValue{ValueString, ""}, nil
	}

	strLength := 0
	for i := start; i < len(chunk); i++ {
		if chunk[i] >= '0' && chunk[i] <= '9' {
			strLength = strLength*10 + int(chunk[i]-'0')
		} else if chunk[i] == ':' {
			// next str_length bytes are our string
			if len(chunk) < i+strLength {
				return i, BencodeValue{}, io.ErrUnexpectedEOF
			}
			result := string(chunk[i+1 : i+1+strLength])
			return i + 1 + strLength, BencodeValue{ValueString, result}, nil
		} else {
			return i, BencodeValue{}, UnexpectedByte(chunk[i])
		}
	}
	return len(chunk), BencodeValue{}, io.ErrUnexpectedEOF
}

func decodeArray(chunk []byte, start int) (int, BencodeValue, error) {
	pos_ := start + 1 // ignore 'l'
	resultArr := make([]BencodeValue, 0)
	for {
		if chunk[pos_] == 'e' {
			return pos_ + 1, BencodeValue{ValueArray, resultArr}, nil
		}

		pos, val, err := decodeInternal(chunk, pos_)
		if err != nil {
			return pos, val, err
		}
		resultArr = append(resultArr, val)
		pos_ = pos
	}
}

func decodeDict(chunk []byte, start int) (int, BencodeValue, error) {
	pos := start + 1 // ignore 'd'
	resultDict := make(map[string]BencodeValue)

	for {
		if chunk[pos] == 'e' {
			return pos + 1, BencodeValue{ValueDict, resultDict}, nil
		}

		var key, value BencodeValue
		var err error

		pos, key, err = decodeString(chunk, pos)
		if err != nil {
			return pos, key, err
		}
		pos, value, err = decodeInternal(chunk, pos)
		if err != nil {
			return pos, value, err
		}
		resultDict[key.value.(string)] = value
	}
}
