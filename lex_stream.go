package main

type charGroup []byte

var linebreak = charGroup{'\r', '\n'}
var whitespace = charGroup{' ', '\t'}
var quotes = charGroup{'\'', '"'}
var paramDelim = append(charGroup{',', ';'}, linebreak...)
var wordDelim = append(append(charGroup{':'}, whitespace...), paramDelim...)
var insDelim = append(charGroup{'='}, wordDelim...)
var shuntDelim = append(charGroup{
	'+', '-', '*', '/', '|', '(', ')', '[', ']', '<', '>', ':', '&', '"', '\'',
}, whitespace...)
var segmentDelim = append(charGroup{'\'', '"'}, whitespace...)

// nestLevelEnter and nestLevelLeave map the various punctuation marks used in
// TASM's syntax to bit flags ordered by their respective nesting priorities.
var nestLevelEnter = map[byte]int{
	'{':  1,
	'(':  2,
	'<':  4,
	'"':  8,
	'\'': 8,
}
var nestLevelLeave = map[byte]int{
	'}':  1,
	')':  2,
	'>':  4,
	'"':  8,
	'\'': 8,
}

func (g *charGroup) matches(b byte) bool {
	for _, v := range *g {
		if v == b {
			return true
		}
	}
	return false
}

// lexStream provides methods to iteratively read through a byte stream using
// delimiter characters.
type lexStream struct {
	input string
	c     int // Current character within the input string
	line  uint
}

const eof = 0

// ignore consumes bytes from the input until they stop matching the given
// character group.
func (s *lexStream) ignore(delim *charGroup) {
	for delim.matches(s.peek()) {
		s.next()
	}
}

// peek returns but does not consume the next byte in the input.
func (s *lexStream) peek() byte {
	if s.c >= len(s.input) {
		return eof
	}
	return s.input[s.c]
}

// next consumes the next byte in the input.
func (s *lexStream) next() byte {
	ret := s.peek()
	s.c++
	if ret == '\n' {
		s.line++
	}
	return ret
}

// nextAssert consumes the next byte in the input and returns a warning if it
// is not equal to b.
func (s *lexStream) nextAssert(b byte, prev string) *ErrorList {
	if ret := s.next() == b; !ret {
		return ErrorListF(ESWarning, "missing a closing %c: %s", b, prev)
	}
	return nil
}

// peekUntil returns but does not consume the next word that is delimited by
// the given character group.
func (s *lexStream) peekUntil(delim *charGroup) string {
	tmp := *s
	return tmp.nextUntil(delim)
}

// nextUntil consumes the next word that is delimited by the given character group.
func (s *lexStream) nextUntil(delim *charGroup) string {
	s.ignore(&whitespace)
	start := s.c
	for !delim.matches(s.peek()) && s.peek() != eof {
		s.next()
	}
	return s.input[start:s.c]
}

// nextToken works like nextUntil, but consumes one additional character if
// the returned string would have been empty.
func (s *lexStream) nextToken(delim *charGroup) string {
	ret := s.nextUntil(delim)
	if len(ret) == 0 {
		ret = string(s.next())
	}
	return ret
}

// nextSegmentParam returns the next token delimited by either whitespace
// or quotes.
func (s *lexStream) nextSegmentParam() (string, *ErrorList) {
	var err *ErrorList
	ret := s.nextUntil(&segmentDelim)
	if next := s.peek(); len(ret) == 0 && quotes.matches(next) {
		nextStr := string(s.next())
		ret = nextStr + s.nextUntil(&charGroup{next})
		err = s.nextAssert(next, ret)
		ret += nextStr
	}
	return ret, err
}

// nextParam consumes and returns the next parameter to an instruction, taking
// nesting into account.
func (s *lexStream) nextParam() string {
	var quote byte
	level := 0

	s.ignore(&whitespace)
	start := s.c
	for !(level == 0 && paramDelim.matches(s.peek())) && s.peek() != eof {
		b := s.next()

		if level == 0 && b == '\\' {
			s.nextUntil(&linebreak)
			s.ignore(&linebreak)
		}
		var leavecond bool
		ll := nestLevelLeave[b]
		if quote != 0 {
			leavecond = (b == quote)
		} else {
			leavecond = (level & ll) != 0
		}
		if leavecond {
			level &= ^ll
			quote = 0
		} else if le := nestLevelEnter[b]; le > level {
			level |= le
			if b == '\'' || b == '"' {
				quote = b
			}
		}
	}
	for s.c > start && whitespace.matches(s.input[s.c-1]) {
		s.c--
	}
	return s.input[start:s.c]
}

func newLexStream(input string) *lexStream {
	return &lexStream{input: input, line: 1}
}
