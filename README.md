# repl

A Go implementation of a Read-Eval-Print-Loop (REPL) with command line editing.

The editing commands are a small subset of readline, using emacs-like bindings.

To use the lib, call the REPL function , passing a handler to it. The handler is called with the Eval method,
and the result is (obj, more, err). If err is non-nil, the error is reported. Otherwise, if more is true, then
more lines are input without printing the result. Eventually, when the handler has accumulated enough to
produce an object, it returns the whole thing as obj (with more == false, and err == nil).

## Example usage

    package main
    
    import (
            "errors"
            "strings"
            "github.com/boynton/repl"
    )
    
    type TestHandler struct {
            value string
    }
    
    //test incomplete lines by counting parens -- they must match.
    func (th *TestHandler) Eval(expr string) (interface{}, bool, error) {
            whole := th.value + expr
            opens := len(strings.Split(whole, "("))
            closes := len(strings.Split(whole, ")"))
            if opens > closes {
                    th.value = whole + " "
                    return nil, true, nil
            } else if closes > opens {
                    th.value = ""
                    return nil, false, errors.New("Unbalanced ')'")
            } else {
                    th.value = ""
                    return whole, false, nil
            }
    }
    
    func main() {
            REPL(new(TestHandler))
    }

