package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/high-moctane/go-readerer"
)

// ReadLineBufSize is buffer size for Nextword.ReadLine
var ReadLineBufSize = 10000 // 10000 is the fastest value (?)

// NextwordParams is a Nextword parameter.
type NextwordParams struct {
	DataPath     string
	CandidateNum int  // Number of candidates
	Greedy       bool // If true, Nextword suggests words from all n-gram data.
}

// Nextword suggests next English words.
type Nextword struct {
	params          *NextwordParams
	readLineBufSize int
}

// NewNextword returns new Nextword. If params is not valid, err will be not nil.
func NewNextword(params *NextwordParams) (*Nextword, error) {
	// nil check
	if params == nil {
		return nil, errors.New("invalid params")
	}

	// DataPath
	fi, err := os.Stat(params.DataPath)
	if err != nil {
		return nil, errors.New(`"NEXTWORD_DATA_PATH" environment variable is not set`)
	}
	if !fi.IsDir() {
		return nil, errors.New(`invalid "NEXTWORD_DATA_PATH"`)
	}

	// candidate-num
	if params.CandidateNum <= 0 {
		return nil, errors.New("candidate-num must be a positive integer")
	}

	return &Nextword{
		params:          params,
		readLineBufSize: ReadLineBufSize,
	}, nil
}

// Suggest suggests next English words from input. If input ends with " ",
// it returns all likely words. If not, it returns the words that begins the last
// word of input.
func (nw *Nextword) Suggest(input string) (candidates []string, err error) {
	ngram, prefix := nw.parseInput(input)

	// search n-gram
	for i := 0; i < len(ngram); i++ {
		var cand []string
		cand, err = nw.searchNgram(ngram[i:])
		if err != nil {
			return
		}

		// merge
		if prefix != "" {
			cand = nw.filterCandidates(cand, prefix)
		}
		candidates = nw.mergeCandidates(candidates, cand)

		// end condition
		if len(candidates) > nw.params.CandidateNum {
			candidates = candidates[:nw.params.CandidateNum]
		}
		if !nw.params.Greedy && len(candidates) > 0 {
			return
		}
	}

	// search 1-gram
	// cand, err := nw.searchOneGram(prefix)
	cand, err := nw.searchDictionary(prefix)

	if err != nil {
		return
	}
	candidates = nw.mergeCandidates(candidates, cand)
	if len(candidates) > nw.params.CandidateNum {
		candidates = candidates[:nw.params.CandidateNum]
	}

	return
}

// parseInput returns last ngram and prefix in the input.
func (*Nextword) parseInput(input string) (ngram []string, prefix string) {
	elems := strings.Split(input, " ")

	// If elems does not end with " ", the word will be prefix.
	if elems[len(elems)-1] != "" {
		prefix = elems[len(elems)-1]
		elems = elems[:len(elems)-1]
	}

	// collect up to last four words.
	for i := len(elems) - 1; i >= 0; i-- {
		if elems[i] == "" {
			continue
		}
		ngram = append([]string{elems[i]}, ngram...)
		if len(ngram) >= 4 {
			break
		}
	}

	return
}

// searchNgram search next English from ngram.
func (nw *Nextword) searchNgram(ngram []string) (candidates []string, err error) {
	fname, ok := nw.ngramFileName(ngram)
	if !ok {
		return
	}

	// open
	path := filepath.Join(nw.params.DataPath, fname)
	if _, err = os.Stat(path); err != nil {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := os.Stat(path)
	if err != nil {
		return
	}

	// search
	query := strings.Join(ngram, " ") + "\t"
	offset, err := nw.binarySearch(f, 0, fi.Size(), query)
	if err != nil {
		err = nw.removeEOF(err)
		return
	}

	line, err := nw.readLine(f, offset)
	if err != nil {
		err = nw.removeEOF(err)
		return
	}
	if !strings.HasPrefix(line, query) {
		return
	}
	candidates = strings.Split(strings.Split(line, "\t")[1], " ")

	return
}

// ngramFileName returns appropriate file name from ngram.
func (*Nextword) ngramFileName(ngram []string) (fname string, ok bool) {
	initial := strings.ToLower(string([]rune(ngram[0])[0]))
	if initial < "a" && "z" < initial {
		return
	}

	fname = fmt.Sprintf("%dgram-%s.txt", len(ngram)+1, initial)
	ok = true
	return
}

// searchOneGram search English words that begins with prefix.
func (nw *Nextword) searchOneGram(prefix string) (candidates []string, err error) {
	if prefix == "" {
		return
	}

	// open
	path := filepath.Join(nw.params.DataPath, "1gram.txt")
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := os.Stat(path)
	if err != nil {
		return
	}

	// search offset
	offset, err := nw.binarySearch(f, 0, fi.Size(), prefix)
	if err != nil {
		if err == io.EOF {
			err = nil
		}
		return
	}

	// collect
	r := readerer.FromReaderAt(f, offset)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, prefix) {
			break
		}
		candidates = append(candidates, line)
	}
	if sc.Err() != nil {
		err = sc.Err()
		return
	}

	return
}

// searchDictionary search English word
func (nw *Nextword) searchDictionary(prefix string) (candidates []string, err error) {
	if prefix == "" {
		return
	}

	// open
	path := filepath.Join(nw.params.DataPath, "dict.txt")
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := os.Stat(path)
	if err != nil {
		return
	}

	query := prefix + "\t"

	// search offset
	offset, err := nw.binarySearch(f, 0, fi.Size(), query)
	if err != nil {
		if err == io.EOF {
			err = nil
		}
		return
	}

	// collect
	r := readerer.FromReaderAt(f, offset)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, query) {
			break
		}
		candidates = append(candidates, line)
	}
	if sc.Err() != nil {
		err = sc.Err()
		return
	}

	return
}

// binarySearch searches query from r between left to right.
func (nw *Nextword) binarySearch(r io.ReaderAt, left, right int64, query string) (offset int64, err error) {
	for left <= right {
		mid := left + (right-left)/2
		if mid == 0 {
			offset = 0
		} else {
			var str string
			str, err = nw.readLine(r, mid)
			if err != nil {
				err = nw.removeEOF(err)
				return
			}
			offset = mid + int64(len(str)) + 1 // "\n"
		}

		var line string
		line, err = nw.readLine(r, offset)
		if err != nil {
			err = nw.removeEOF(err)
			return
		}

		if query < line {
			right = mid - 1
		} else if query == line {
			return
		} else {
			left = mid + 1
		}
	}

	mid := left + (right-left)/2
	if mid == 0 {
		offset = 0
	} else {
		var str string
		str, err = nw.readLine(r, mid)
		if err != nil {
			err = nw.removeEOF(err)
			return
		}
		offset = mid + int64(len(str)) + 1 // "\n"
	}

	return
}

// readLine reads r from offset until "\n".
func (nw *Nextword) readLine(r io.ReaderAt, offset int64) (string, error) {
	strBuilder := new(strings.Builder)

	for {
		buf := make([]byte, nw.readLineBufSize)
		n, err := r.ReadAt(buf, offset)
		if err == io.EOF && n == 0 {
			return "", err
		} else if err != nil && err != io.EOF {
			strBuilder.Write(buf[:n])
			return strBuilder.String(), err
		}

		strBuilder.Write(buf[:n])

		// return when buf has "\n"
		for i, b := range buf[:n] {
			if b == '\n' {
				return strBuilder.String()[:strBuilder.Len()-n+i], nil
			}
		}

		offset += int64(n)
	}
}

// removeEOF removes io.EOF from err.
func (*Nextword) removeEOF(err error) error {
	if err == io.EOF {
		return nil
	}
	return err
}

// mergeCandidates merges two candidates
func (*Nextword) mergeCandidates(a, b []string) []string {
	ret := a[:]

	m := map[string]bool{}
	for _, str := range a {
		m[str] = true
	}

	for _, str := range b {
		if !m[str] {
			m[str] = true
			ret = append(ret, str)
		}
	}

	return ret
}

// filterCandidates filters words which do not begin with prefix from cand.
func (*Nextword) filterCandidates(cand []string, prefix string) []string {
	res := make([]string, 0, len(cand))

	for _, str := range cand {
		if !strings.HasPrefix(str, prefix) {
			continue
		}
		res = append(res, str)
	}

	return res
}
