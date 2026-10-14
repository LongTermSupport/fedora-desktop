package tarstate

import (
	"fmt"
	"strconv"
	"strings"
)

// The standard library's tar writer will not emit several headers a hostile tar can hold
// (a pax path over a different ustar name, a regular file with a trailing slash, sparse
// records), so these tests assemble the 512-byte blocks themselves.

func octal(width int, v int64) string {
	return fmt.Sprintf("%0*o\x00", width-1, v)
}

// rawBlock is one ustar header block.
func rawBlock(name string, typeflag byte, mode, size int64) []byte {
	b := make([]byte, 512)
	copy(b[0:100], name)
	copy(b[100:108], octal(8, mode))
	copy(b[108:116], octal(8, 0))
	copy(b[116:124], octal(8, 0))
	copy(b[124:136], octal(12, size))
	copy(b[136:148], octal(12, 0))
	for i := 148; i < 156; i++ {
		b[i] = ' '
	}
	b[156] = typeflag
	copy(b[257:263], "ustar\x00")
	copy(b[263:265], "00")
	sum := 0
	for _, c := range b {
		sum += int(c)
	}
	copy(b[148:156], fmt.Sprintf("%06o\x00 ", sum))
	return b
}

// rawData is data padded to a whole number of blocks.
func rawData(data string) []byte {
	n := (len(data) + 511) / 512 * 512
	b := make([]byte, n)
	copy(b, data)
	return b
}

// rawEntry is a header and its data.
func rawEntry(name string, typeflag byte, mode int64, data string) []byte {
	return append(rawBlock(name, typeflag, mode, int64(len(data))), rawData(data)...)
}

// paxRecord is one "<length> key=value\n" record, the length counting itself.
func paxRecord(key, value string) string {
	body := " " + key + "=" + value + "\n"
	n := len(body) + 1
	for len(strconv.Itoa(n))+len(body) != n {
		n = len(strconv.Itoa(n)) + len(body)
	}
	return strconv.Itoa(n) + body
}

// rawPax is an extended header followed by the entry it modifies.
func rawPax(records map[string]string, entry []byte) []byte {
	var sb strings.Builder
	for k, v := range records {
		sb.WriteString(paxRecord(k, v))
	}
	return append(rawEntry("PaxHeader/x", 'x', 0o644, sb.String()), entry...)
}

// rawTar joins entries and adds the end-of-archive marker.
func rawTar(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return append(out, make([]byte, 1024)...)
}
