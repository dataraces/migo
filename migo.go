package migo

import (
	"bytes"
	"fmt"
	"go/token"
	"log"
	"strings"
)

var (
	nameFilter = strings.NewReplacer("(", "", ")", "", "*", "", "/", "_", "\"", "", "-", "")
	noPosition = token.Position{Line: 0}
)

// NamedVar is a named variable.
type NamedVar interface {
	Name() string
	String() string
}

// Program is a set of Functions in a program.
type Program struct {
	Funcs   []*Function // Function definitions.
	visited map[*Function]int
}

// NewProgram creates a new empty Program.
func NewProgram() *Program {
	return &Program{Funcs: []*Function{}}
}

// AddFunction adds a Function to Program.
//
// If Function already exists this does nothing.
func (p *Program) AddFunction(f *Function) {
	for _, fun := range p.Funcs {
		if fun.Name == f.Name {
			return
		}
	}
	p.Funcs = append(p.Funcs, f)
}

// Function gets a Function in a Program by name.
//
// Returns the function and a bool indicating whether lookup was successful.
func (p *Program) Function(name string) (*Function, bool) {
	for _, f := range p.Funcs {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}

// findEmptyFuncMain marks functions empty if they do not have communication.
func (p *Program) findEmptyFuncMain(f *Function) {
	known := make(map[string]bool)
	p.findEmptyFunc(f, known)
	f.HasComm = true
}

// findEmptyFunc marks functions empty if they do not have communication.
// takes a map of known functions in parameter.
func (p *Program) findEmptyFunc(f *Function, known map[string]bool) {
	if _, ok := known[f.Name]; ok {
		return
	}
	known[f.Name] = f.HasComm
	for _, stmt := range f.Stmts {
		switch stmt := stmt.(type) {
		case *CallStatement:
			if child, ok := p.Function(stmt.Name); ok {
				if hasComm, ok := known[child.Name]; ok {
					f.HasComm = f.HasComm || hasComm
				} else {
					p.findEmptyFunc(child, known)
					f.HasComm = f.HasComm || child.HasComm
				}
				known[f.Name] = f.HasComm
			}
		case *SpawnStatement:
			if child, ok := p.Function(stmt.Name); ok {
				if hasComm, ok := known[child.Name]; ok {
					f.HasComm = f.HasComm || hasComm
				} else {
					p.findEmptyFunc(child, known)
					f.HasComm = f.HasComm || child.HasComm
				}
				known[f.Name] = f.HasComm
			}
		}
	}
}

func (p *Program) String() string {
	var buf bytes.Buffer
	for _, f := range p.Funcs {
		if !f.IsEmpty() {
			buf.WriteString(f.String())
		}
	}
	return buf.String()
}

func (p Program) PrintWithProperties(props Properties) string {
	var buf bytes.Buffer
	main, _ := p.Function("\"main\".main")

	buf.WriteString(main.PrintWithProperties(props))

	for _, f := range p.Funcs {
		if f.SimpleName() != "main.main" &&
			!strings.HasPrefix(f.SimpleName(), "os") &&
			!strings.HasPrefix(f.SimpleName(), "syscall") &&
			!strings.HasPrefix(f.SimpleName(), "internal_poll") &&
			!strings.HasPrefix(f.SimpleName(), "sync.o") {
			buf.WriteString(f.PrintWithProperties(props))
		}
	}

	if !props.IsEmpty() {
		log.Fatalf("Unable to find target location of properties: %v", props.Values())
	}

	return buf.String()
}

// Parameter is a translation from caller environment to callee.
type Parameter struct {
	Caller NamedVar
	Callee NamedVar
}

func (p *Parameter) String() string {
	return fmt.Sprintf("[%s → %s]", p.Caller.Name(), p.Callee.Name())
}

// CalleeParameterString converts a slice of *Parameter to parameter string.
func CalleeParameterString(params []*Parameter) string {
	var buf bytes.Buffer
	for i, p := range params {
		if i == 0 {
			buf.WriteString(p.Callee.Name())
		} else {
			buf.WriteString(fmt.Sprintf(", %s", p.Callee.Name()))
		}
	}
	return buf.String()
}

// CallerParameterString converts a slice of *Parameter to parameter string.
func CallerParameterString(params []*Parameter) string {
	var buf bytes.Buffer
	for i, p := range params {
		if i == 0 {
			buf.WriteString(p.Caller.Name())
		} else {
			buf.WriteString(fmt.Sprintf(", %s", p.Caller.Name()))
		}
	}
	return buf.String()
}

// Function is a block of Statements sharing the same parameters.
type Function struct {
	Name    string       // Name of the function.
	Params  []*Parameter // Parameters (map from local variable name to Parameter).
	Stmts   []Statement  // Function body (slice of statements).
	HasComm bool         // Does the function has communication statement?

	stack  *StmtsStack    // Stack for working with nested conditionals.
	Pos    token.Position // Position of the function in Go source code.
	varIdx int            // Next fresh variable index.
}

// NewFunction creates a new Function using the given name.
func NewFunction(name string, pos token.Position) *Function {
	return &Function{
		Name:   name,
		Params: []*Parameter{},
		Stmts:  []Statement{},
		stack:  NewStmtsStack(),
		Pos:    pos,
	}
}

// AddParams adds Parameters to Function.
//
// If Parameter already exists this does nothing.
func (f *Function) AddParams(params ...*Parameter) {
	for _, param := range params {
		found := false
		for _, p := range f.Params {
			if p.Callee == param.Callee || p.Caller == param.Caller {
				found = true
			}
		}
		if !found {
			f.Params = append(f.Params, param)
		}
	}
}

// GetParamByCalleeValue is for looking up params from the body of a Function.
func (f *Function) GetParamByCalleeValue(v NamedVar) (*Parameter, error) {
	for _, p := range f.Params {
		if p.Callee == v {
			return p, nil
		}
	}
	return nil, fmt.Errorf("Parameter not found")
}

// SimpleName returns a filtered name of a function.
func (f *Function) SimpleName() string {
	return nameFilter.Replace(f.Name)
}

// AddStmts add Statement(s) to a Function.
func (f *Function) AddStmts(stmts ...Statement) {
	numStmts := len(f.Stmts)
	if numStmts > 1 {
		if _, ok := f.Stmts[numStmts-1].(*TauStatement); ok {
			f.Stmts = append(f.Stmts[:numStmts], stmts...)
			return
		}
	}
	if !f.HasComm {
		if hasComm(stmts) {
			f.HasComm = true
		}
	}
	f.Stmts = append(f.Stmts, stmts...)
}

func hasComm(stmts []Statement) bool {
	for _, s := range stmts {
		switch s := s.(type) {
		case *SendStatement, *RecvStatement, *CloseStatement, *SelectStatement, *NewChanStatement:
			return true
		case *IfStatement:
			return hasComm(s.Then) || hasComm(s.Else)
		case *IfForStatement:
			return hasComm(s.Then) || hasComm(s.Else)
		case *CallStatement:
			return len(s.Params) > 0
		case *SpawnStatement:
			return len(s.Params) > 0
		}
	}
	return false
}

// IsEmpty returns true if the Function body is empty.
func (f *Function) IsEmpty() bool { return len(f.Stmts) == 0 }

// PutAway pushes current statements to stack.
func (f *Function) PutAway() {
	f.stack.Push(f.Stmts)
	f.Stmts = []Statement{}
}

// Restore pops current statements from stack.
func (f *Function) Restore() ([]Statement, error) { return f.stack.Pop() }

func (f *Function) String() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("def %s(%s):\n",
		f.SimpleName(), CalleeParameterString(f.Params)))
	if len(f.Stmts) == 0 {
		f.AddStmts(&TauStatement{})
	}
	for _, stmt := range f.Stmts {
		buf.WriteString(fmt.Sprintf("    %s;\n", stmt))
	}
	return buf.String()
}

func (f *Function) PrintWithProperties(props Properties) string {
	var buf bytes.Buffer
	for _, prop := range props.GetProperties(f.Pos) {
		buf.WriteString(fmt.Sprintf("%s\n", prop))
	}
	buf.WriteString(fmt.Sprintf("def %s(%s):\n",
		f.SimpleName(), CalleeParameterString(f.Params)))
	if len(f.Stmts) == 0 {
		f.AddStmts(&TauStatement{})
	}
	for _, stmt := range f.Stmts {
		for _, prop := range props.GetProperties(stmt.Position()) {
			buf.WriteString(fmt.Sprintf("    %s\n", prop))
		}
		buf.WriteString(fmt.Sprintf("    %s;\n", stmt))
	}
	return buf.String()
}

// Statement is a generic statement.
type Statement interface {
	String() string
	Position() token.Position
}

// CallStatement captures function calls or block jumps in the SSA.
type CallStatement struct {
	Name   string
	Params []*Parameter
	Pos    token.Position
}

// SimpleName returns a filtered name.
func (s *CallStatement) SimpleName() string {
	return nameFilter.Replace(s.Name)
}

func (s *CallStatement) String() string {
	return fmt.Sprintf("call %s(%s)",
		s.SimpleName(), CallerParameterString(s.Params))
}

// AddParams add parameter(s) to a Function call.
func (s *CallStatement) AddParams(params ...*Parameter) {
	for _, param := range params {
		found := false
		for _, p := range s.Params {
			if p == param {
				found = true
			}
		}
		if !found {
			s.Params = append(s.Params, param)
		}
	}
}

func (s *CallStatement) Position() token.Position {
	return s.Pos
}

// CloseStatement closes a channel.
type CloseStatement struct {
	Chan string // Channel name
	Pos  token.Position
}

func (s *CloseStatement) String() string {
	return fmt.Sprintf("close %s", s.Chan)
}

func (s *CloseStatement) Position() token.Position {
	return s.Pos
}

// SpawnStatement captures spawning of goroutines.
type SpawnStatement struct {
	Name   string
	Params []*Parameter
	Pos    token.Position
}

// SimpleName returns a filtered name.
func (s *SpawnStatement) SimpleName() string {
	return nameFilter.Replace(s.Name)
}

func (s *SpawnStatement) String() string {
	return fmt.Sprintf("spawn %s(%s)",
		s.SimpleName(), CallerParameterString(s.Params))
}

// AddParams add parameter(s) to a goroutine spawning Function call.
func (s *SpawnStatement) AddParams(params ...*Parameter) {
	for _, param := range params {
		found := false
		for _, p := range s.Params {
			if p == param {
				found = true
			}
		}
		if !found {
			s.Params = append(s.Params, param)
		}
	}
}

func (s *SpawnStatement) Position() token.Position {
	return s.Pos
}

// NewChanStatement creates and names a newly created channel.
type NewChanStatement struct {
	Name NamedVar
	Chan string
	Size int64
	Pos  token.Position
}

func (s *NewChanStatement) String() string {
	return fmt.Sprintf("let %s = newchan %s, %d",
		s.Name.Name(), nameFilter.Replace(s.Chan), s.Size)
}

func (s *NewChanStatement) Position() token.Position {
	return s.Pos
}

// IfStatement is a conditional statement.
//
// IfStatements always have both Then and Else.
type IfStatement struct {
	Then []Statement
	Else []Statement
}

func (s *IfStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString("if ")
	for _, t := range s.Then {
		buf.WriteString(fmt.Sprintf("%s; ", t.String()))
	}
	buf.WriteString("else ")
	for _, f := range s.Else {
		buf.WriteString(fmt.Sprintf("%s; ", f.String()))
	}
	buf.WriteString("endif")
	return buf.String()
}

func (s *IfStatement) Position() token.Position {
	return noPosition
}

// IfForStatement is a conditional statement introduced by a for-loop.
//
// IfForStatements always have both Then and Else.
type IfForStatement struct {
	ForCond string // Condition of the loop
	Then    []Statement
	Else    []Statement
}

func (s *IfForStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("ifFor (int %s) then ", s.ForCond))
	for _, t := range s.Then {
		buf.WriteString(fmt.Sprintf("%s; ", t.String()))
	}
	buf.WriteString("else ")
	for _, f := range s.Else {
		buf.WriteString(fmt.Sprintf("%s; ", f.String()))
	}
	buf.WriteString("endif")
	return buf.String()
}

func (s *IfForStatement) Position() token.Position {
	return noPosition
}

// SelectStatement is non-deterministic choice
type SelectStatement struct {
	Cases [][]Statement
	Pos   token.Position
}

func (s *SelectStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("select"))
	for _, c := range s.Cases {
		buf.WriteString("\n      case")
		for _, stmt := range c {
			buf.WriteString(fmt.Sprintf(" %s;", stmt.String()))
		}
	}
	buf.WriteString("\n    endselect")
	return buf.String()
}

func (s *SelectStatement) Position() token.Position {
	return s.Pos
}

// TauStatement is inaction.
type TauStatement struct{}

func (s *TauStatement) String() string {
	return "tau"
}

func (s *TauStatement) Position() token.Position {
	return noPosition
}

// SendStatement sends to Chan.
type SendStatement struct {
	Chan string
	Pos  token.Position
}

func (s *SendStatement) String() string {
	return fmt.Sprintf("send %s", s.Chan)
}

func (s *SendStatement) Position() token.Position {
	return s.Pos
}

// RecvStatement receives from Chan.
type RecvStatement struct {
	Chan string
	Pos  token.Position
}

func (s *RecvStatement) String() string {
	return fmt.Sprintf("recv %s", s.Chan)
}

func (s *RecvStatement) Position() token.Position {
	return s.Pos
}

// NewMem creates a new memory or variable reference.
type NewMem struct {
	Name NamedVar
	Pos  token.Position
}

func (s *NewMem) String() string {
	return fmt.Sprintf("letmem %s", s.Name.Name())
}

func (s *NewMem) Position() token.Position {
	return s.Pos
}

// MemRead is a memory read statement.
type MemRead struct {
	Name string
	Pos  token.Position
}

func (s *MemRead) String() string {
	return fmt.Sprintf("read %s", nameFilter.Replace(s.Name))
}

func (s *MemRead) Position() token.Position {
	return s.Pos
}

// MemWrite is a memory write statement.
type MemWrite struct {
	Name string
	Pos  token.Position
}

func (s *MemWrite) String() string {
	return fmt.Sprintf("write %s", nameFilter.Replace(s.Name))
}

func (s *MemWrite) Position() token.Position {
	return s.Pos
}

// Mutex primitives

// NewSyncMutex is a sync.Mutex initialisation statement.
type NewSyncMutex struct {
	Name NamedVar
	Pos  token.Position
}

func (m *NewSyncMutex) String() string {
	return fmt.Sprintf("letsync %s mutex", m.Name.Name())
}

func (s *NewSyncMutex) Position() token.Position {
	return s.Pos
}

// SyncMutexLock is a sync.Mutex Lock statement.
type SyncMutexLock struct {
	Name string
	Pos  token.Position
}

func (m *SyncMutexLock) String() string {
	return fmt.Sprintf("lock %s", nameFilter.Replace(m.Name))
}

func (s *SyncMutexLock) Position() token.Position {
	return s.Pos
}

// SyncMutexUnlock is a sync.Mutex Unlock statement.
type SyncMutexUnlock struct {
	Name string
	Pos  token.Position
}

func (m *SyncMutexUnlock) String() string {
	return fmt.Sprintf("unlock %s", nameFilter.Replace(m.Name))
}

func (s *SyncMutexUnlock) Position() token.Position {
	return s.Pos
}

// RWMutex primitives

// NewSyncRWMutex is a sync.RWMutex initialisation statement.
type NewSyncRWMutex struct {
	Name NamedVar
	Pos  token.Position
}

func (m *NewSyncRWMutex) String() string {
	return fmt.Sprintf("letsync %s rwmutex", m.Name.Name())
}

func (s *NewSyncRWMutex) Position() token.Position {
	return s.Pos
}

// SyncRWMutexRLock is a sync.RWMutex RLock statement.
type SyncRWMutexRLock struct {
	Name string
	Pos  token.Position
}

func (m *SyncRWMutexRLock) String() string {
	return fmt.Sprintf("rlock %s", nameFilter.Replace(m.Name))
}

func (s *SyncRWMutexRLock) Position() token.Position {
	return s.Pos
}

// SyncRWMutexRUnlock is a sync.RWMutex RUnlock statement.
type SyncRWMutexRUnlock struct {
	Name string
	Pos  token.Position
}

func (m *SyncRWMutexRUnlock) String() string {
	return fmt.Sprintf("runlock %s", nameFilter.Replace(m.Name))
}

func (s *SyncRWMutexRUnlock) Position() token.Position {
	return s.Pos
}

// Maps source code line number to property comments
type Properties map[int][]string

// Associate properties to line in the source code
func (ps Properties) AddProperties(prop []string, line int) {
	ps[line] = append(ps[line], prop...)
}

// Gets properties for a spesific line. Returned properties are removed from the structure
func (ps Properties) GetProperties(postition token.Position) []string {
	props := ps[postition.Line]
	delete(ps, postition.Line)
	return props
}

// Non destructivly gets all properties in structure
func (ps Properties) Values() []string {
	vals := make([]string, 0)
	for _, v := range ps {
		vals = append(vals, v...)
	}
	return vals
}

func (ps Properties) IsEmpty() bool {
	return len(ps) == 0
}
