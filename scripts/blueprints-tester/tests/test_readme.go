package tests

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/util"
)

const (
	blockStart = "```javascript\n"
	bashStart  = "```bash\n"
	blockEnd   = "```\n"
	// Matches lines like `[//]: # (b1)`.
	blockIDPattern = "^\\[//\\]: # \\((b\\d+)\\)\\W*$"
	// Matches lines like `<!-- (<code>) -->`
	hiddenCodePattern = "<!--\\s*(.*)\\s*-->\\W*$"

	// workDir is the directory blueprints are placed during testing.
	workDir = "/tmp/kelda-blueprint-test"
)

var errUnbalanced = errors.New("unbalanced code blocks")

type readmeParser struct {
	currentBlock string
	// Map block ID to code block.
	codeBlocks map[string]string
	recording  bool
	ignoring   bool
}

func (parser *readmeParser) parse(line string) error {
	isStart := line == blockStart
	isEnd := line == blockEnd
	isBash := line == bashStart

	hiddenCodeMatch, isHidden := getMatch(hiddenCodePattern, line)
	blockIDMatch, isBlockID := getMatch(blockIDPattern, line)

	if (isStart && parser.recording) ||
		(isEnd && !parser.ignoring && !parser.recording) {
		return errUnbalanced
	}

	switch {
	case isBlockID:
		parser.currentBlock = blockIDMatch
	case isHidden:
		line = hiddenCodeMatch + "\n"
		break
	case isStart:
		parser.recording = true

		if parser.currentBlock == "" {
			return errors.New("missing code block id")
		}

		if _, ok := parser.codeBlocks[parser.currentBlock]; !ok {
			parser.codeBlocks[parser.currentBlock] = ""
		}
	case isBash:
		parser.ignoring = true
	case isEnd:
		parser.recording = false
		parser.ignoring = false
		parser.currentBlock = ""
	}

	if (parser.recording && !isStart) || isHidden {
		parser.codeBlocks[parser.currentBlock] += line
	}

	return nil
}

func (parser readmeParser) blocks() (map[string]string, error) {
	if parser.recording {
		return nil, errUnbalanced
	}
	return parser.codeBlocks, nil
}

var dependencies = `{
  "dependencies": {
    "kelda": "0.x",
    "@kelda/redis": "kelda/redis"
  }
}`

// TestReadme checks that the code snippets in the README compile.
func TestReadme() error {
	f, err := util.Open("../../README.md")
	if err != nil {
		return fmt.Errorf("failed to open README: %s", err.Error())
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	parser := readmeParser{}
	parser.codeBlocks = make(map[string]string)

	for scanner.Scan() {
		if err := parser.parse(scanner.Text() + "\n"); err != nil {
			return fmt.Errorf("failed to parse README: %s",
				err.Error())
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read README: %s", err.Error())
	}

	blocks, err := parser.blocks()
	if err != nil {
		return fmt.Errorf("failed to parse README: %s", err.Error())
	}

	os.Mkdir(workDir, 0755)
	defer os.RemoveAll(workDir)
	os.Chdir(workDir)
	util.WriteFile(filepath.Join(workDir, "package.json"), []byte(dependencies), 0644)
	if err := run("npm", "install", "."); err != nil {
		return err
	}

	for _, block := range blocks {
		blueprintPath := filepath.Join(workDir, "readme_block.js")
		util.WriteFile(blueprintPath, []byte(block), 0644)
		if _, err := blueprint.FromFile(blueprintPath); err != nil {
			return err
		}
	}
	return nil
}

func getMatch(pattern, line string) (string, bool) {
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(line)
	if len(match) > 0 {
		return match[1], true
	}
	return "", false
}
