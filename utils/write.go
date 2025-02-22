package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/smallstep/cli/command"
	"github.com/smallstep/cli/errs"
	"github.com/smallstep/cli/ui"
)

var (
	// ErrFileExists is the error returned if a file exists.
	ErrFileExists = errors.New("file exists")

	// ErrIsDir is the error returned if the file is a directory.
	ErrIsDir = errors.New("file is a directory")

	// SnippetHeader is the header of a step generated snippet in a
	// configuration file.
	SnippetHeader = "# autogenerated by step"

	// SnippetFooter is the header of a step generated snippet in a
	// configuration file.
	SnippetFooter = "# end"
)

// WriteFile wraps ioutil.WriteFile with a prompt to overwrite a file if
// the file exists. It returns ErrFileExists if the user picks to not overwrite
// the file. If force is set to true, the prompt will not be presented and the
// file if exists will be overwritten.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	if command.IsForce() {
		return ioutil.WriteFile(filename, data, perm)
	}

	st, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return ioutil.WriteFile(filename, data, perm)
		}
		return errors.Wrapf(err, "error reading information for %s", filename)
	}

	if st.IsDir() {
		return ErrIsDir
	}

	str, err := ui.Prompt(fmt.Sprintf("Would you like to overwrite %s [y/n]", filename), ui.WithValidateYesNo())
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(str)) {
	case "y", "yes":
	case "n", "no":
		return ErrFileExists
	}

	return ioutil.WriteFile(filename, data, perm)
}

// AppendNewLine appends the given data at the end of the file. If the last
// character of the file does not contain an LF it prepends it to the data.
func AppendNewLine(filename string, data []byte, perm os.FileMode) error {
	f, err := OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, perm)
	if err != nil {
		return err
	}
	// Read last character
	if st, err := f.File.Stat(); err == nil && st.Size() != 0 {
		last := make([]byte, 1)
		f.Seek(-1, 2)
		f.Read(last)
		if last[0] != '\n' {
			f.WriteString("\n")
		}
	}
	f.Write(data)
	return f.Close()
}

// WriteSnippet writes the given data on the given filename. It surrounds the
// data with a header and footer, and it will replace the previous one.
func WriteSnippet(filename string, data []byte, perm os.FileMode) error {
	// Get file permissions
	if st, err := os.Stat(filename); err == nil {
		perm = st.Mode()
	} else if !os.IsNotExist(err) {
		return errs.FileError(err, filename)
	}

	// Read file contents
	b, err := ioutil.ReadFile(filename)
	if err != nil && !os.IsNotExist(err) {
		return errs.FileError(err, filename)
	}

	// Detect previous configuration
	_, start, end := findConfiguration(bytes.NewReader(b))

	// Replace previous configuration
	f, err := OpenFile(filename, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, perm)
	if err != nil {
		return errs.FileError(err, filename)
	}
	if len(b) > 0 {
		f.Write(b[:start])
		if start == end {
			f.WriteString("\n")
		}
	}
	f.WriteString(fmt.Sprintf("%s @ %s\n", SnippetHeader, time.Now().UTC().Format(time.RFC3339)))
	f.Write(data)
	if !bytes.HasSuffix(data, []byte("\n")) {
		f.WriteString("\n")
	}
	f.WriteString(SnippetFooter + "\n")
	if len(b) > 0 {
		f.Write(b[end:])
	}
	return f.Close()
}

type offsetCounter struct {
	offset int64
}

func (o *offsetCounter) ScanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	advance, token, err = bufio.ScanLines(data, atEOF)
	o.offset += int64(advance)
	return
}

func findConfiguration(r io.Reader) (lines []string, start, end int64) {
	var inConfig bool
	counter := new(offsetCounter)
	scanner := bufio.NewScanner(r)
	scanner.Split(counter.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case !inConfig && strings.HasPrefix(line, SnippetHeader):
			inConfig = true
			start = counter.offset - int64(len(line)+1)
		case inConfig && strings.HasPrefix(line, SnippetFooter):
			return lines, start, counter.offset
		case inConfig:
			lines = append(lines, line)
		}
	}

	return lines, counter.offset, counter.offset
}
