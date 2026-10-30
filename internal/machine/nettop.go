package machine

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// byteCounts are a process's cumulative bytes as nettop reports them.
type byteCounts struct {
	in  int64
	out int64
}

// parseNettop reads `nettop -P -L 1 -x -J bytes_in,bytes_out` CSV. The first
// column is `name.pid`; names are truncated by nettop and may contain dots,
// so the pid is whatever follows the last dot.
func parseNettop(text string) map[int]byteCounts {
	out := map[int]byteCounts{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		dot := strings.LastIndex(fields[0], ".")
		if dot < 0 {
			continue
		}
		pid, err := strconv.Atoi(fields[0][dot+1:])
		if err != nil {
			continue
		}
		in, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		o, _ := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		out[pid] = byteCounts{in: in, out: o}
	}
	return out
}

// nettopBlocks reads nettop's logging output (-L 0): each sample starts with
// a header line whose first column is empty. Every completed sample is handed
// to emit as a fresh map.
func nettopBlocks(r io.Reader, emit func(map[int]byteCounts)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var block []string
	flush := func() {
		if len(block) > 0 {
			emit(parseNettop(strings.Join(block, "\n")))
			block = nil
		}
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, ",") {
			flush()
		}
		block = append(block, line)
	}
	flush()
}
