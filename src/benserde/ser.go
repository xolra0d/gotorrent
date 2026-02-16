package benserde

import (
	"fmt"
	"sort"
	"strconv"
)

func EncodeBencode(decoded BencodeValue) []byte {
	buffer := make([]byte, 0)
	encodeInternal(decoded, &buffer)
	return buffer
}

func encodeInternal(decoded BencodeValue, buffer *[]byte) {
	switch decoded.vType {
	case ValueInt:
		encodeInt(decoded.value.(int), buffer)
	case ValueString:
		encodeString(decoded.value.(string), buffer)
	case ValueArray:
		encodeArray(decoded.value.([]BencodeValue), buffer)
	case ValueDict:
		encodeDict(decoded.value.(map[string]BencodeValue), buffer)
	default:
		fmt.Println("unknown type")
	}
}

func encodeInt(decoded int, buffer *[]byte) {
	*buffer = append(*buffer, 'i')
	*buffer = strconv.AppendInt(*buffer, int64(decoded), 10)
	*buffer = append(*buffer, 'e')
}

func encodeString(decoded string, buffer *[]byte) {
	*buffer = strconv.AppendInt(*buffer, int64(len(decoded)), 10)
	*buffer = append(*buffer, ':')
	*buffer = append(*buffer, []byte(decoded)...)
}

func encodeArray(decoded []BencodeValue, buffer *[]byte) {
	*buffer = append(*buffer, 'l')
	for _, el := range decoded {
		encodeInternal(el, buffer)
	}
	*buffer = append(*buffer, 'e')
}

func encodeDict(decoded map[string]BencodeValue, buffer *[]byte) {
	*buffer = append(*buffer, 'd')
	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		encodeString(key, buffer)
		encodeInternal(decoded[key], buffer)
	}
	*buffer = append(*buffer, 'e')
}
