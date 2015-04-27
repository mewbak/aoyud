// Shunting-yard parsing of arithmetic expressions.

package main

import (
	"fmt"
	"strings"
)

// Eh, why not, helps debugging.
type shuntOpID string

const (
	opPlus  = "+"
	opMinus = "-"
	opMul   = "*"
	opDiv   = "/"
	opMod   = "mod"
	opShL   = "shl"
	opShR   = "shr"

	opAnd = "and"
	opOr  = "or"
	opXor = "xor"

	opEq = "eq"
	opNe = "ne"
	opLt = "lt"
	opLe = "le"
	opGt = "gt"
	opGe = "ge"

	opNot = "not"

	opParenL = "("
	opParenR = ")"

	opPtr = "ptr"
)

type shuntVal interface {
	calc(retStack *shuntStack) (shuntVal, *ErrorList)
	fmt.Stringer
}

func (v asmInt) calc(retStack *shuntStack) (shuntVal, *ErrorList) {
	return v, nil
}

func (v asmString) calc(retStack *shuntStack) (shuntVal, *ErrorList) {
	return v.toInt()
}

type shuntOp struct {
	id         shuntOpID
	precedence int
	args       int
	// Function to apply to the two operands.
	// a will be pushed back onto the stack.
	function func(a, b *asmInt)
}

func (op *shuntOp) Thing() string {
	return "arithmetic operator"
}

func (op *shuntOp) width() uint {
	return 0
}

func (op *shuntOp) calc(retStack *shuntStack) (shuntVal, *ErrorList) {
	var args [2]asmInt
	for i := 0; i < op.args; i++ {
		arg, err := retStack.pop()
		if err != nil {
			return arg, err
		}
		args[1-i] = arg.(asmInt)
	}
	op.function(&args[0], &args[1])
	return args[0], nil
}

func (op *shuntOp) String() string {
	return string(op.id)
}

type shuntOpMap map[string]shuntOp
type shuntStack []shuntVal

func (stack *shuntStack) push(element shuntVal) {
	*stack = append(*stack, element)
}

func (stack *shuntStack) peek() shuntVal {
	if length := len(*stack); length != 0 {
		return (*stack)[length-1]
	}
	return nil
}

func (stack *shuntStack) pop() (shuntVal, *ErrorList) {
	if ret := stack.peek(); ret != nil {
		*stack = (*stack)[:len(*stack)-1]
		return ret, nil
	}
	return nil, ErrorListF(ESError, "arithmetic stack underflow")
}

// Why, Go, why.
func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

var asmTypes = map[string]asmInt{
	"BYTE":  {n: 1},
	"WORD":  {n: 2},
	"DWORD": {n: 4},
	"PWORD": {n: 6},
	"FWORD": {n: 6},
	"QWORD": {n: 8},
	"TBYTE": {n: 10},
}

var unaryOperators = shuntOpMap{
	"(":   {opParenL, 14, 0, nil},
	")":   {opParenR, 14, 0, nil},
	"+":   {opPlus, 8, 1, func(a, b *asmInt) { a.base = b.base }},
	"-":   {opMinus, 8, 1, func(a, b *asmInt) { a.n = -b.n; a.base = b.base }},
	"NOT": {opNot, 4, 1, func(a, b *asmInt) { a.n = ^b.n; a.base = b.base }},
}

var binaryOperators = shuntOpMap{
	"(": {opParenL, 14, 0, nil},
	")": {opParenR, 14, 0, nil},
	"PTR": {opPtr, 11, 2, func(a, b *asmInt) {
		a.ptr = uint64(a.n)
		a.n = b.n
		a.base = b.base
	}},
	"*":   {opMul, 7, 2, func(a, b *asmInt) { a.n *= b.n }},
	"/":   {opDiv, 7, 2, func(a, b *asmInt) { a.n /= b.n }},
	"MOD": {opMod, 7, 2, func(a, b *asmInt) { a.n %= b.n }},
	"SHR": {opShR, 7, 2, func(a, b *asmInt) { a.n >>= uint(b.n) }},
	"SHL": {opShL, 7, 2, func(a, b *asmInt) { a.n <<= uint(b.n) }},
	"+":   {opPlus, 6, 2, func(a, b *asmInt) { a.n += b.n }},
	"-":   {opMinus, 6, 2, func(a, b *asmInt) { a.n -= b.n }},
	"EQ":  {opEq, 5, 2, func(a, b *asmInt) { a.n = b2i(a.n == b.n) }},
	"NE":  {opNe, 5, 2, func(a, b *asmInt) { a.n = b2i(a.n != b.n) }},
	"LT":  {opLt, 5, 2, func(a, b *asmInt) { a.n = b2i(a.n < b.n) }},
	"LE":  {opLe, 5, 2, func(a, b *asmInt) { a.n = b2i(a.n <= b.n) }},
	"GT":  {opGt, 5, 2, func(a, b *asmInt) { a.n = b2i(a.n > b.n) }},
	"GE":  {opGe, 5, 2, func(a, b *asmInt) { a.n = b2i(a.n >= b.n) }},
	"AND": {opAnd, 3, 2, func(a, b *asmInt) { a.n &= b.n }},
	"OR":  {opOr, 2, 2, func(a, b *asmInt) { a.n |= b.n }},
	"|":   {opOr, 2, 2, func(a, b *asmInt) { a.n |= b.n }},
	"XOR": {opXor, 2, 2, func(a, b *asmInt) { a.n ^= b.n }},
}

// nextShuntToken returns the next operand or operator from s. Only operators
// in opSet are identified as such.
func (s *SymMap) nextShuntToken(stream *lexStream, opSet *shuntOpMap) (ret asmVal, err *ErrorList) {
	token := stream.nextToken(&shuntDelim)
	if isAsmInt(token) {
		return newAsmInt(token)
	} else if quote := token[0]; quotes.matches(quote) && len(token) == 1 {
		token = stream.nextUntil(&charGroup{quote})
		err = stream.nextAssert(quote, token)
		return asmString(token), err
	}
	tokenUpper := strings.ToUpper(token)
	if typ, ok := asmTypes[tokenUpper]; ok {
		return typ, err
	} else if nextOp, ok := (*opSet)[tokenUpper]; ok {
		return &nextOp, nil
	}
	return s.Get(token)
}

// pushOp evaluates newOp, a newly incoming operator, in relation to the
// previous operators on top of opStack, and returns the next set of allowed
// operators.
func (retStack *shuntStack) pushOp(opStack *shuntStack, newOp *shuntOp) (*shuntOpMap, *ErrorList) {
	switch newOp.id {
	case opParenR:
		top, err := opStack.pop()
		for top != nil && top.(*shuntOp).id != opParenL {
			retStack.push(top)
			top, err = opStack.pop()
		}
		if top == nil {
			err = err.AddF(ESError, "mismatched parentheses")
		}
		return &binaryOperators, err
	case opParenL:
		opStack.push(newOp)
	default:
		for top := opStack.peek(); top != nil; top = opStack.peek() {
			op := top.(*shuntOp)
			if op.id == opParenL || newOp.precedence <= op.precedence {
				break
			}
			retStack.push(op)
			opStack.pop()
		}
		opStack.push(newOp)
	}
	return &unaryOperators, nil
}

type shuntState struct {
	retStack shuntStack
	opStack  shuntStack
	opSet    *shuntOpMap
}

func (s *SymMap) shuntLoop(state *shuntState, pos ItemPos, expr string) (err *ErrorList) {
	stream := NewLexStreamAt(pos, expr)
	for stream.peek() != eof && err.Severity() < ESError {
		token, errToken := s.nextShuntToken(stream, state.opSet)
		err = err.AddL(errToken)
		if errToken.Severity() >= ESError {
			return err
		}
		switch token.(type) {
		case asmInt:
			state.retStack.push(token.(asmInt))
			state.opSet = &binaryOperators
		case asmString:
			state.retStack.push(token.(asmString))
			state.opSet = &binaryOperators
		case *shuntOp:
			var errOp *ErrorList
			state.opSet, errOp = state.retStack.pushOp(
				&state.opStack, token.(*shuntOp),
			)
			err.AddL(errOp)
		case asmExpression:
			err = err.AddL(
				s.shuntLoop(state, pos, string(token.(asmExpression))),
			)
		default:
			err = err.AddF(ESError,
				"can't use %s in arithmetic expression", token.Thing(),
			)
		}
		stream.ignore(&whitespace)
	}
	return err
}

// shunt converts the arithmetic expression in expr into an RPN stack.
func (s *SymMap) shunt(pos ItemPos, expr string) (stack *shuntStack, err *ErrorList) {
	state := &shuntState{opSet: &unaryOperators}
	if err = s.shuntLoop(state, pos, expr); err.Severity() >= ESError {
		return nil, err
	}
	for top := state.opStack.peek(); top != nil; top = state.opStack.peek() {
		state.opStack.pop()
		if top.(*shuntOp).id == opParenL {
			err = err.AddF(ESError, "missing a right parenthesis")
		} else {
			state.retStack.push(top)
		}
	}
	return &state.retStack, err
}

// solve evaluates the RPN stack s and returns the result.
func (s shuntStack) solve() (ret *asmInt, err *ErrorList) {
	retStack := make(shuntStack, 0, cap(s))
	for _, val := range s {
		result, errCalc := val.calc(&retStack)
		if errCalc.Severity() < ESError {
			retStack.push(result)
		}
		err = err.AddL(errCalc)
	}
	if len(retStack) != 1 {
		return nil, err.AddF(ESError, "invalid RPN expression: %s", s)
	}
	result := retStack[0].(asmInt)
	return &result, err
}

// evalInt wraps shunt and solve.
func (s *SymMap) evalInt(pos ItemPos, expr string) (*asmInt, *ErrorList) {
	rpnStack, err := s.shunt(pos, expr)
	if err.Severity() < ESError {
		ret, errSolve := rpnStack.solve()
		return ret, err.AddL(errSolve)
	}
	return nil, err
}

// evalBool wraps evalInt and casts its result to a bool.
func (s *SymMap) evalBool(pos ItemPos, expr string) (bool, *ErrorList) {
	ret, err := s.evalInt(pos, expr)
	if err.Severity() < ESError {
		return ret.n != 0, err
	}
	// Default to false in the case of an error... for now, at least.
	return false, err
}
