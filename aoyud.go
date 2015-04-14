/*
 * As of yet unnamed assembly-to-C decompiler.
 * Implemented in a similar fashion as the lexer from Go's own text/template
 * package.
 */

package main

import (
	"fmt"
	"gopkg.in/alecthomas/kingpin.v1"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

type SourcePos struct {
	filename *string
	line     uint // 0 = EOF
}

func (p SourcePos) String() string {
	if p.line == 0 {
		return fmt.Sprintf("%s(EOF):", *p.filename)
	} else {
		return fmt.Sprintf("%s(%d):", *p.filename, p.line)
	}
}

type ItemPos []SourcePos

func (p *ItemPos) String() string {
	ret := ""
	if p == nil {
		return ret
	}
	for i, pos := range *p {
		if i != 0 {
			ret += "\n" + strings.Repeat(" ", i)
		}
		ret += pos.String()
	}
	return ret + " "
}

type itemParams []string

func (p itemParams) String() string {
	return strings.Join(p, ", ")
}

// item represents a token or text string returned from the scanner.
type item struct {
	pos    ItemPos    // Code position of this item and the macros it came from.
	typ    itemType   // The type of this item
	sym    string     // Optional symbol name
	val    string     // Name of the instruction or label. Limited to ASCII characters.
	params itemParams // Instruction parameters
}

// itemType identifies the type of lex items.
type itemType int

const (
	itemError       itemType = iota // error occurred; value is text of error
	itemLabel                       // jump target
	itemInstruction                 // instruction or directive and its parameters
)

// Range defines a range of numbers. Negative values for Max indicate no upper
// limit.
type Range struct {
	Min, Max int
}

// checkParamRange returns nil if the number of parameters in the item is
// within the given range, or an error message if it isn't.
func (it *item) checkParamRange(r Range) *ErrorList {
	var sev ErrorSeverity
	given := len(it.params)
	below := given < r.Min
	if below || uint(given) > uint(r.Max) {
		var textErr, textParams string
		if below {
			if given > 0 {
				textParams = ": " + it.params.String()
			}
			textErr = fmt.Sprintf(
				"requires at least %d parameters, %d given", r.Min, given,
			) + textParams
			sev = ESError
		} else {
			if r.Max == 0 {
				textParams = "accepts no parameters"
			} else {
				textParams = fmt.Sprintf("accepts a maximum of %d parameters", r.Max)
			}
			extra := given - r.Max
			textErr = textParams + fmt.Sprintf(
				", ignoring %d additional ones: ", extra,
			) + strings.Join(it.params[given-extra:], ", ")
			sev = ESWarning
		}
		return ErrorListF(sev, "%s %s", it.val, textErr)
	}
	return nil
}

type lexer struct {
	stream   lexStream
	filename *string
	paths    []string  // search paths for relative includes
	curInst  item      // current instruction being built
	items    chan item // channel of scanned items
}

type stateFn func(*lexer) stateFn

func INCLUDE(l *lexer, it *item) *ErrorList {
	// Remember to restore the old filename once we're done with this one
	newL, err := lexFile(it.params[0], l.paths)
	if err != nil {
		return err
	}
	for i := range newL.items {
		l.items <- i
	}
	return nil
}

// lexFirst scans labels, the symbol declaration, and the name of the
// instruction.
func lexFirst(l *lexer) stateFn {
	first := l.stream.nextUntil(&insDelim)
	// Label?
	if l.stream.peek() == ':' {
		l.stream.next()
		l.emitItem(&item{typ: itemLabel, sym: first})
		return lexFirst
	} else
	// Assignment? (Needs to be a special case because = doesn't need to be
	// surrounded by spaces, and nextUntil() isn't designed to handle that.)
	if l.stream.peek() == '=' {
		l.newInstruction(first, string(l.stream.next()))
		return lexParam
	}

	var secondRule SymRule
	second := l.stream.peekUntil(&wordDelim)
	firstUpper := strings.ToUpper(first)
	if _, ok := Keywords[firstUpper]; ok {
		first = firstUpper
	} else {
		secondUpper := strings.ToUpper(second)
		if k, ok := Keywords[secondUpper]; ok {
			second = secondUpper
			secondRule = k.Sym
		}
	}

	if secondRule != NotAllowed {
		l.newInstruction(first, second)
		l.stream.nextUntil(&wordDelim)
	} else if firstUpper == "COMMENT" {
		l.stream.ignore(&whitespace)
		delim := charGroup{l.stream.next()}
		l.stream.nextUntil(&delim)
		l.stream.nextUntil(&linebreak) // Yes, everything else on the line is ignored.
		return lexFirst
	} else {
		l.newInstruction("", first)
	}
	return lexParam
}

// lexParam scans parameters and comments.
func lexParam(l *lexer) stateFn {
	if param := l.stream.nextParam(); len(param) > 0 {
		l.curInst.params = append(l.curInst.params, param)
	}
	switch l.stream.next() {
	case ';', '\\':
		// Comment
		l.stream.nextUntil(&linebreak)
		return lexFirst
	case '\r', '\n':
		return lexFirst
	case eof:
		return nil
	}
	return lexParam
}

// newInstruction emits the currently cached instruction and starts a new one
// with the given symbol and value.
func (l *lexer) newInstruction(sym, val string) {
	var err *ErrorList

	l.curInst.typ = itemInstruction

	if k, ok := Keywords[l.curInst.val]; ok && k.Lex != nil {
		err = l.curInst.checkParamRange(k.ParamRange)
		if err.Severity() < ESError {
			err = err.AddL(k.Lex(l, &l.curInst))
		}
	} else {
		l.emit(&l.curInst)
	}
	if err != nil {
		l.curInst.pos[0].line = l.stream.line
		l.curInst.pos.ErrorPrint(err)
	}
	l.curInst.sym = sym
	l.curInst.val = val
	l.curInst.params = nil
}

// emit sets the position of the given item and sends it over the items channel.
func (l *lexer) emit(it *item) {
	if len(it.val) > 0 || len(it.sym) > 0 {
		it.pos = ItemPos{SourcePos{filename: l.filename, line: l.stream.line}}
		l.items <- *it
	}
}

// emitItem emits the currently cached instruction, followed by the given item.
func (l *lexer) emitItem(it *item) {
	l.newInstruction("", "")
	l.emit(it)
}

// run runs the state machine for the lexer.
func (l *lexer) run() {
	for state := lexFirst; state != nil; {
		state = state(l)
	}
	l.newInstruction("", "") // send the currently cached instruction
	close(l.items)
}

// readFirstFromPaths reads and returns the contents of a file with name
// filename from the first directory in the given list that contains such a
// file, the full path to the file that was read, as well as any error that
// occurred.
func readFirstFromPaths(filename string, paths []string) (string, string, *ErrorList) {
	for _, path := range paths {
		fullname := filepath.Join(path, filename)
		bytes, err := ioutil.ReadFile(fullname)
		if err == nil {
			return string(bytes), fullname, nil
		} else if !os.IsNotExist(err) {
			return "", "", NewErrorList(ESFatal, err)
		}
	}
	return "", "", ErrorListF(ESFatal,
		"could not find %s in any of the source paths:\n\t%s",
		filename, strings.Join(paths, "\n\t"),
	)
}

func lexFile(filename string, paths []string) (*lexer, *ErrorList) {
	bytes, fullname, err := readFirstFromPaths(filename, paths)
	if err != nil {
		return nil, err
	}
	l := &lexer{
		filename: &filename,
		stream:   *newLexStream(bytes),
		items:    make(chan item),
		paths:    append(paths, filepath.Dir(fullname)),
	}
	go l.run()
	return l, nil
}

func (it item) String() string {
	var ret string
	switch it.typ {
	case itemLabel:
		ret = it.sym + ":"
	case itemInstruction:
		if it.sym != "" {
			ret = it.sym
		}
		ret += "\t" + it.val
	}
	if len(it.params) > 0 {
		ret += "\t" + it.params.String()
	}
	return ret
}

func main() {
	filename := kingpin.Arg(
		"filename", "Assembly file.",
	).Required().ExistingFile()

	syntax := kingpin.Flag(
		"syntax", "Target assembler.",
	).Default("TASM").Enum("TASM", "MASM")

	includes := kingpin.Flag(
		"include", "Add the given directory to the list of assembly include directories.",
	).Default(".").Short('I').Strings()

	kingpin.Parse()

	l, err := lexFile(*filename, *includes)
	if err != nil {
		PosNull.ErrorPrint(err)
	}
	p := NewParser(*syntax)

	for i := range l.items {
		p.eval(&i)
	}
	for _, i := range p.instructions {
		fmt.Println(i)
	}
	p.end()
	PosNull.ErrorPrint(ErrorListF(ESDebug, "%s", p.syms))
}
