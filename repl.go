package repl

import (
	"fmt"
	"syscall"
	"unsafe"
)

// State contains the state of a terminal.
type State struct {
	termios syscall.Termios
}

// IsTerminal returns true if the given file descriptor is a terminal.
func IsTerminal(fd int) bool {
	var termios syscall.Termios
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), uintptr(getTermios), uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return err == 0
}

// MakeRaw put the terminal connected to the given file descriptor into raw
// mode and returns the previous state of the terminal so that it can be
// restored.
func MakeRaw(fd int) (*State, error) {
	var oldState State
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), uintptr(getTermios), uintptr(unsafe.Pointer(&oldState.termios)), 0, 0, 0); err != 0 {
		return nil, err
	}

	newState := oldState.termios
	newState.Iflag &^= syscall.ISTRIP | syscall.INLCR | syscall.ICRNL | syscall.IGNCR | syscall.IXON | syscall.IXOFF
	newState.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), uintptr(setTermios), uintptr(unsafe.Pointer(&newState)), 0, 0, 0); err != 0 {
		return nil, err
	}

	return &oldState, nil
}

// Restore restores the terminal connected to the given file descriptor to a
// previous state.
func Restore(fd int, state *State) error {
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), uintptr(setTermios), uintptr(unsafe.Pointer(&state.termios)), 0, 0, 0)
	return err
}

func GetChar() (byte, error) {
	var ch [1]byte
	n, err := syscall.Read(syscall.Stdout, ch[:])
	if err != nil || n == 0 {
		return 0, err
	} else {
		return ch[0], nil
	}
}

func PutChar(b byte) error {
	var ch [1]byte
	ch[0] = b
	_, err := syscall.Write(syscall.Stdout, ch[:])
	return err
}

func PutString(s string) error {
	_, err := syscall.Write(syscall.Stdout, []byte(s))
	return err
}

func cursorBackward() error {
	b := []byte{27, '[', '1', 'D'}
	_, err := syscall.Write(syscall.Stdout, b)
	return err
}

func cursorForward() error {
	b := []byte{27, '[', '1', 'C'}
	_, err := syscall.Write(syscall.Stdout, b)
	return err
}

type LineBuf struct {
	length       int
	cursor       int
	buf          []byte
	yanked       string
	yanking      bool
	history      []string
	historyIndex int
}

func MakeLineBuf(capacity int) LineBuf {
	storage := make([]byte, capacity)
	return LineBuf{0, 0, storage[:], "", false, nil, -1}
}

func (lb *LineBuf) IsEmpty() bool {
	return lb.length == 0
}

func (lb *LineBuf) Clear() {
	lb.length = 0
	lb.cursor = 0
	lb.yanking = false
}

func (lb *LineBuf) Insert(ch byte) {
	lb.yanking = false
	n := len(lb.buf)
	if lb.length == n {
		target := make([]byte, n+10)
		copy(target, lb.buf[:n])
		lb.buf = target
	}
	if lb.cursor == lb.length {
		lb.buf[lb.cursor] = ch
	} else {
		copy(lb.buf[lb.cursor+1:], lb.buf[lb.cursor:])
		lb.buf[lb.cursor] = ch
	}
	lb.cursor = lb.cursor + 1
	lb.length = lb.length + 1
}

func (lb *LineBuf) InsertBytes(chs []byte) {
	for _, ch := range chs {
		lb.Insert(ch)
	}
}

func (lb *LineBuf) Delete() bool {
	lb.yanking = false
	if lb.cursor < lb.length {
		copy(lb.buf[lb.cursor:], lb.buf[lb.cursor+1:])
		lb.length = lb.length - 1
		return true
	} else {
		return false
	}
}

func (lb *LineBuf) KillToEnd() int {
	n := lb.length - lb.cursor
	//for now, a single yank buffer, not a stack
	if lb.yanking {
		lb.yanked = lb.yanked + string(lb.buf[lb.cursor:lb.length])
	} else {
		lb.yanked = string(lb.buf[lb.cursor:lb.length])
	}
	lb.length = lb.cursor
	lb.yanking = false
	return n
}

func (lb *LineBuf) DeleteRange(begin int, end int) int {
	if begin < 0 {
		begin = 0
	} else if begin > lb.length {
		return 0
	}
	if end > lb.length {
		end = lb.length
	} else if end < 0 {
		return 0
	}
	n := end - begin
	if n > 0 {
		if lb.yanking {
			lb.yanked = lb.yanked + string(lb.buf[begin:end])
		} else {
			lb.yanked = string(lb.buf[begin:end])
		}
		copy(lb.buf[begin:], lb.buf[end:])
		lb.length = lb.length - n
		lb.cursor = begin
	}
	return n
}

func (lb *LineBuf) WordBackspace() int {
	var i = lb.cursor
	if lb.cursor > 0 {
		i--
	}
	for ; i > 0; i-- {
		if lb.buf[i] != SPACE {
			break
		}
	}
	if lb.buf[i] != SPACE {
		for ; i > 0; i-- {
			if lb.buf[i] == SPACE {
				return lb.DeleteRange(i+1, lb.cursor)
			}
		}
	}
	return lb.DeleteRange(0, lb.cursor)
}

func (lb *LineBuf) WordDelete() int {
	var i int
	for i = lb.cursor - 1; i < lb.length; i++ {
		if lb.buf[i] != SPACE {
			break
		}
	}
	for ; i < lb.length; i++ {
		if lb.buf[i] == SPACE {
			return lb.DeleteRange(lb.cursor, i)
		}
	}
	return 0
}

func (lb *LineBuf) WordForward() {
	i := lb.cursor
	for ; i < lb.length; i++ {
		if lb.buf[i] != SPACE {
			break
		}
	}
	for ; i < lb.length; i++ {
		if lb.buf[i] == SPACE {
			lb.cursor = i
			return
		}
	}
	lb.cursor = lb.length
}

func (lb *LineBuf) WordBackward() {
	i := lb.cursor
	if lb.cursor > 0 {
		i--
	}
	for ; i > 0; i-- {
		if lb.buf[i] != SPACE {
			break
		}
	}
	if lb.buf[i] != SPACE {
		for ; i > 0; i-- {
			if lb.buf[i] == SPACE {
				lb.cursor = i + 1
				return
			}
		}
	}
	lb.cursor = 0
}

func (lb *LineBuf) Yank() int {
	lb.yanking = true
	lb.InsertBytes([]byte(lb.yanked))
	return len(lb.yanked)

}

func (lb *LineBuf) Backward() bool {
	lb.yanking = false
	if lb.cursor > 0 {
		lb.cursor = lb.cursor - 1
		return true
	} else {
		return false
	}
}

func (lb *LineBuf) Forward() bool {
	lb.yanking = false
	if lb.cursor < lb.length {
		lb.cursor = lb.cursor + 1
		return true
	} else {
		return false
	}
}

func (lb *LineBuf) Begin() {
	lb.yanking = false
	lb.cursor = 0
}

func (lb *LineBuf) End() {
	lb.yanking = false
	lb.cursor = lb.length
}

func (lb *LineBuf) AddToHistory(line string) {
	lb.history = append(lb.history, line)
	lb.historyIndex = -1
}

func (lb *LineBuf) PrevInHistory() int {
	n := lb.length
	if lb.history != nil {
		if lb.historyIndex < 0 {
			lb.historyIndex = len(lb.history) - 1
		} else {
			lb.historyIndex--
		}
		if lb.historyIndex >= 0 {
			lb.length = 0
			lb.cursor = 0
			lb.InsertBytes([]byte(lb.history[lb.historyIndex]))
			if lb.length > n {
				n = lb.length
			}
		} else {
			lb.historyIndex = 0
		}
	}
	return n
}

func (lb *LineBuf) NextInHistory() int {
	n := lb.length
	if lb.history != nil {
		if lb.historyIndex >= 0 {
			lb.historyIndex++
			if lb.historyIndex < len(lb.history) {
				lb.length = 0
				lb.cursor = 0
				lb.InsertBytes([]byte(lb.history[lb.historyIndex]))
				if lb.length > n {
					n = lb.length
				}
			} else {
				lb.historyIndex--
			}
		}
	}
	return n
}

func (lb *LineBuf) String() string {
	return string(lb.buf[0:lb.length])
}

const CTRL_A = 1
const CTRL_B = 2
const CTRL_C = 3
const CTRL_D = 4
const CTRL_E = 5
const CTRL_F = 6
const BEEP = 7
const NEWLINE = 10
const CTRL_K = 11
const CTRL_L = 12
const RETURN = 13
const CTRL_N = 14
const CTRL_P = 16
const CTRL_Y = 25
const ESCAPE = 27
const SPACE = 32
const DELETE = 127

func dump(prompt string, lb LineBuf, extra int) {
	fmt.Println("\ncursor =", lb.cursor, "length =", lb.length)
	for i := 0; i < lb.length; i++ {
		PutChar(lb.buf[i])
	}
	PutChar(NEWLINE)
	for i := 0; i < lb.length; i++ {
		if i == lb.cursor {
			PutChar('^')
		} else {
			PutChar('.')
		}
	}
	if lb.cursor == lb.length {
		PutChar('^')
	}
	PutChar(NEWLINE)
}

func drawline(prompt string, lb LineBuf, extra int) {
	PutChar(13)
	PutString(prompt)
	PutString(lb.String())
	for i := 0; i < extra; i++ {
		PutChar(SPACE)
	}
	cursor := lb.length + extra
	for cursor > lb.cursor {
		cursorBackward()
		cursor = cursor - 1
	}
}

type ReplHandler interface {
	Eval(expr string) (interface{}, bool, error)
	Reset()
	Prompt() string
}

func repl(handler ReplHandler) error {
	buf := MakeLineBuf(1024)
	prompt := handler.Prompt()
	PutString(prompt)
	meta := false
	for true {
		ch, err := GetChar()
		if err != nil {
			return err
		} else if meta {
			meta = false
			switch ch {
			case DELETE:
				n := buf.WordBackspace()
				drawline(prompt, buf, n)
			case 'd':
				n := buf.WordDelete()
				drawline(prompt, buf, n)
			case 'b':
				buf.WordBackward()
				drawline(prompt, buf, 0)
			case 'f':
				buf.WordForward()
				drawline(prompt, buf, 0)
			default:
				PutChar(BEEP)
			}
		} else {
			switch ch {
			case ESCAPE:
				meta = true
			case CTRL_D:
				if buf.IsEmpty() {
					PutString("\n")
					return nil
				} else {
					buf.Delete()
					drawline(prompt, buf, 1)
				}
			case CTRL_A:
				buf.Begin()
				drawline(prompt, buf, 0)
			case CTRL_E:
				buf.End()
				drawline(prompt, buf, 0)
			case CTRL_F:
				if buf.Forward() {
					cursorForward()
					drawline(prompt, buf, 0)
				}
			case CTRL_B:
				if buf.Backward() {
					cursorBackward()
					drawline(prompt, buf, 0)
				}
			case CTRL_C:
				PutString("*** Interrupt ***\n")
				buf.Clear()
				handler.Reset()
				PutString(prompt)
			case CTRL_K:
				n := buf.KillToEnd()
				drawline(prompt, buf, n)
			case CTRL_Y:
				n := buf.Yank()
				drawline(prompt, buf, n)
			case CTRL_L:
				//dump(prompt, buf, 0);
				PutString("\n")
				drawline(prompt, buf, 0)
			case CTRL_N:
				n := buf.NextInHistory()
				drawline(prompt, buf, n)
			case CTRL_P:
				n := buf.PrevInHistory()
				drawline(prompt, buf, n)
			case DELETE:
				if buf.Backward() {
					buf.Delete()
					drawline(prompt, buf, 1)
				}
			case RETURN:
				if !buf.IsEmpty() {
					PutChar('\n')
				}
				s := buf.String()
				buf.AddToHistory(s)
				buf.Clear()
				result, more, err := handler.Eval(s)
				prompt = handler.Prompt()
				if err != nil {
					fmt.Println("***", err)
					buf.Clear()
					PutString(prompt)
				} else if more {
					//PutString("\n (need more)\n")
				} else {
					fmt.Println(result)
					PutString(prompt)
				}
			default:
				if ch >= SPACE && ch < 127 {
					buf.Insert(ch)
					drawline(prompt, buf, 0)
				} else {
					PutChar(BEEP)
				}
			}
		}
		prompt = handler.Prompt()
	}
	return nil
}

func REPL(handler ReplHandler) {
	state, err := MakeRaw(syscall.Stdout)
	if err == nil {
		defer Restore(syscall.Stdout, state)
		repl(handler)
	} else {
		fmt.Println("whoops, cannot set raw mode", err)
	}
}
