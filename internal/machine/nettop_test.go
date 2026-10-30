package machine

import (
	"strings"
	"testing"
)

const nettopSample = `,bytes_in,bytes_out,
syslogd.365,0,4978,
apsd.371,2430122,5989432,
Google Chrome H.1228,7912294,1621856,
com.apple.Safari.9100,55,66,
broken,1,2,
`

func TestParseNettop(t *testing.T) {
	got := parseNettop(nettopSample)
	if len(got) != 4 {
		t.Fatalf("rows: %d %+v", len(got), got)
	}
	if got[365] != (byteCounts{in: 0, out: 4978}) || got[371] != (byteCounts{in: 2430122, out: 5989432}) {
		t.Fatalf("counts: %+v", got)
	}
	if got[1228] != (byteCounts{in: 7912294, out: 1621856}) {
		t.Fatalf("name with spaces: %+v", got[1228])
	}
	if got[9100] != (byteCounts{in: 55, out: 66}) {
		t.Fatalf("name with dots must take the pid after the last dot: %+v", got[9100])
	}
}

func TestNettopBlocks(t *testing.T) {
	stream := ",bytes_in,bytes_out,\na.1,10,20,\nb.2,30,40,\n,bytes_in,bytes_out,\na.1,11,21,\n"
	var got []map[int]byteCounts
	nettopBlocks(strings.NewReader(stream), func(m map[int]byteCounts) { got = append(got, m) })
	if len(got) != 2 || len(got[0]) != 2 || got[0][2].out != 40 || got[1][1].in != 11 || len(got[1]) != 1 {
		t.Fatalf("%+v", got)
	}
}
