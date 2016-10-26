package docs

import (
	"bufio"
	"errors"
	"testing"

	"github.com/NetSys/quilt/stitch"
	"github.com/NetSys/quilt/util"
)

const (
	blockStart = "```javascript\n"
	blockEnd   = "```\n"
)

var errUnbalanced = errors.New("unbalanced code blocks")

type readmeParser struct {
	codeBlocks []string
	recording  bool
}

func (parser *readmeParser) parse(line string) error {
	isStart := line == blockStart
	isEnd := line == blockEnd

	if (isStart && parser.recording) || (isEnd && !parser.recording) {
		return errUnbalanced
	}

	switch {
	case isStart:
		parser.recording = true
		parser.codeBlocks = append(parser.codeBlocks, "")
	case isEnd:
		parser.recording = false
	}

	if parser.recording && !isStart {
		parser.codeBlocks[len(parser.codeBlocks)-1] += line
	}

	return nil
}

func (parser readmeParser) blocks() ([]string, error) {
	if parser.recording {
		return nil, errUnbalanced
	}
	return parser.codeBlocks, nil
}

func TestReadme(t *testing.T) {
	f, err := util.Open("../README.md")
	if err != nil {
		t.Errorf("Failed to open README: %s", err.Error())
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	parser := readmeParser{}

	for scanner.Scan() {
		if err := parser.parse(scanner.Text() + "\n"); err != nil {
			t.Errorf("Failed to parse README: %s", err.Error())
			return
		}
	}

	if err := scanner.Err(); err != nil {
		t.Errorf("Failed to read README: %s", err.Error())
		return
	}

	blocks, err := parser.blocks()
	if err != nil {
		t.Errorf("Failed to parse README: %s", err.Error())
		return
	}

	for _, block := range blocks {
		if err = checkConfig(block); err != nil {
			t.Errorf(err.Error())
		}
	}
}

func checkConfig(content string) error {
	_, err := stitch.New(content, stitch.DefaultImportGetter)
	if err != nil {
		return err
	}
	return nil
}
