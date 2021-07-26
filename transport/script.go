package transport

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
	"github.com/robertkrimen/otto"
	_ "github.com/robertkrimen/otto/underscore"
)

var errHalt = errors.New("timed out")

func init() {
	RegisterTransporter("script", func() Transporter { return &Script{} })
}

// Interpreter is a wrapper around the Otto JS interpreter, with
// added extensions specific to the use-cases for upload server
// custom scripting
type Interpreter struct {
	vm   *otto.Otto
	user model.UserModel
}

// NewInterpreter creates a new intialized Interpreter instance
func NewInterpreter(u model.UserModel) Interpreter {
	vm := otto.New()
	obj := Interpreter{vm: vm, user: u}
	obj.Initialize()
	return obj
}

// GetContext retrieves the current user context associated with the
// running JS interpreter
func (obj Interpreter) GetContext() context.Context {
	return context.Background()
	//return user.NewContext(context.Background(), &obj.user)
}

// Initialize runs the necessary tasks for the JS interpreter to be usable
func (obj *Interpreter) Initialize() {
	// log(): Log to the upload server log
	obj.vm.Set("log", func(call otto.FunctionCall) otto.Value {
		passedVal, _ := call.Argument(0).ToString()
		log.Printf("JS.VM: %s: %s", obj.user.Username, passedVal)
		return otto.Value{}
	})

	{
		mailObj := new(mail)
		mailObj.obj = obj
		obj.vm.Set("mail", mailObj)
	}

	{
		httpObj := new(httpclient)
		httpObj.obj = obj
		obj.vm.Set("http", httpObj)
	}

}

// RunUnsafe runs potentially unsafe javascript code with a timeout of a
// certain number of seconds
func (obj *Interpreter) RunUnsafe(code string, timeout int) (e error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if caught := recover(); caught != nil {
			if caught == errHalt {
				log.Printf("js.Interpreter.RunUnsafe(): Timed out after %d sec", timeout)
				e = fmt.Errorf("js.Interpreter.RunUnsafe(): Timed out after %d sec", timeout)
				return
			}
			panic(caught)
		}
		log.Printf("js.Interpreter.RunUnsafe(): Completed execution in %v", duration)
	}()

	obj.vm.Interrupt = make(chan func(), 1)
	go func(obj *Interpreter, timeout int) {
		time.Sleep(time.Duration(timeout) * time.Second)
		obj.vm.Interrupt <- func() {
			panic(errHalt)
		}
	}(obj, timeout)

	log.Printf("js.Interpreter.RunUnsafe(): Beginning execution")
	_, err := obj.vm.Run(code)
	if err != nil {
		log.Printf("js.Interpreter.RunUnsafe(): Returned %#v", err)
	}
	return err
}

type Script struct {
	script  string
	timeout int
	ctx     context.Context
}

// Transport performs the actual work of transport, given the input.
func (s *Script) Transport(filename string, data interface{}) error {
	// TODO: FIXME: IMPLEMENT: XXX

	// Retrieve user from context
	um, ok := user.FromContext(s.ctx)
	if !ok {
		return fmt.Errorf("unable to retrieve user from context")
	}

	// Load script from string
	js := NewInterpreter(*um)

	// Prepopulate all of the input data
	js.Initialize()

	// Run the script
	err := js.RunUnsafe(s.script, s.timeout)

	return err
}

// InputFormat returns the input format required by this plugin.
func (s *Script) InputFormat() string {
	return "x12"
}

// Options returns a list of valid options for this transporter type
func (s *Script) Options() []string {
	return []string{"script", "timeout"}
}

func (s *Script) SetContext(c context.Context) error {
	s.ctx = c
	return nil
}

// SetOptions sets the current options for this plugin
func (s *Script) SetOptions(o map[string]interface{}) error {
	s.script, _ = s.coerceOptionString(o, "script")
	s.timeout, _ = s.coerceOptionInt(o, "timeout")

	return nil
}

func (s *Script) coerceOptionString(o map[string]interface{}, keyname string) (string, error) {
	x, ok := o[keyname]
	if !ok {
		return "", fmt.Errorf("unable to read option for '%s'", keyname)
	}
	y, ok := x.(string)
	if !ok {
		return "", fmt.Errorf("unable to coerce value for '%s'", keyname)
	}
	return y, nil
}

func (s *Script) coerceOptionInt(o map[string]interface{}, keyname string) (int, error) {
	x, ok := o[keyname]
	if !ok {
		return 0, fmt.Errorf("unable to read option for '%s'", keyname)
	}
	y, ok := x.(int)
	if !ok {
		return 0, fmt.Errorf("unable to coerce value for '%s'", keyname)
	}
	return y, nil
}
