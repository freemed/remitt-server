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
	// The legacy database and UI refer to this plugin by its Java class name,
	// ScriptedHttpTransport (migrations/001_legacy.up.sql); jobqueue passes
	// that value straight through, so the alias must resolve too.
	registerJavaTransporter("ScriptedHttpTransport", func() Transporter { return &Script{} })
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

	// explicit holds the options a direct caller handed to SetOptions. They
	// take precedence over the values stored for the user in tUserConfig (see
	// configureFromUser and selfconfig.go).
	explicit map[string]any
}

// Transport performs the actual work of transport, given the input.
func (s *Script) Transport(filename string, data any) error {
	// Retrieve user from context
	um, ok := user.FromContext(s.ctx)
	if !ok {
		return fmt.Errorf("script: unable to retrieve user from context")
	}

	// Resolve the configuration before it is used: the caller's own
	// tUserConfig rows are applied through SetOptions' coercion path, and
	// anything a direct caller set explicitly beats them.
	if err := s.configureFromUser(); err != nil {
		return err
	}

	// Load script from string
	js := NewInterpreter(*um)

	// Prepopulate all of the input data
	js.Initialize()

	if s.script == "" {
		return fmt.Errorf("script: no script option given")
	}

	// Import the data passed via interface
	js.vm.Set("data", data)

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

// userConfigNamespaces returns the tUserConfig namespaces this plugin's own
// rows live under, the Java FQCN the legacy database stores first and the
// registered short name second.
//
// The legacy namespace 'org.remitt.plugin.transport.ScriptedHttpTransport'
// also holds username/password rows (migrations/001_legacy.up.sql:68-69), which
// are NOT options this plugin declares: only script/timeout are applied, and
// the foreign keys are left alone.
func (s *Script) userConfigNamespaces() []string {
	return transportNamespaces("script", "ScriptedHttpTransport")
}

// storedOptionValue converts one stored tUserConfig value into the Go value
// SetOptions expects. timeout is the plugin's only int option; script is text.
func (s *Script) storedOptionValue(option, stored string) (any, error) {
	if option == "timeout" {
		return storedIntOption(option, stored)
	}
	return storedStringOption(option, stored)
}

// configureFromUser reads the caller's own configuration out of tUserConfig and
// applies it, so the plugin runs the script the user actually configured
// without anything having to call SetOptions.
//
// Explicitly-set options always win over database-derived ones; the database
// only fills in what SetOptions did not supply. With no user in the context, or
// no database, there is no database-derived configuration and the plugin keeps
// exactly what SetOptions gave it (see selfconfig.go).
func (s *Script) configureFromUser() error {
	stored, err := loadUserConfigOptions(s.ctx, "script", s.userConfigNamespaces(), s.Options(), s.storedOptionValue)
	if err != nil {
		return fmt.Errorf("script: %w", err)
	}
	return s.applyOptions(mergeOptions(stored, s.explicit))
}

// SetOptions sets the current options for this plugin.
//
// An option that is present but carries the wrong type is reported, naming the
// option: the previous implementation discarded the coercion error
// (s.script, _ = ...), so a wrong-typed option left the plugin silently
// unconfigured and only failed later, in Transport, with "script: no script
// option given" - an error that never named the option actually at fault.
//
// An option that is simply absent is not an error: the field keeps its zero
// value and Transport reports the missing script.
//
// The options handed in here are remembered as the explicitly-set ones: they
// take precedence, option by option, over the values the caller has stored for
// this plugin in tUserConfig, which Transport applies on top of what is missing
// (see configureFromUser).
func (s *Script) SetOptions(o map[string]any) error {
	s.explicit = copyOptions(o)
	return s.applyOptions(o)
}

// applyOptions is the option-coercion path: it maps an option map onto the
// plugin's own fields, reporting any value it cannot coerce.
func (s *Script) applyOptions(o map[string]any) error {
	var err error
	if s.script, err = s.stringOption(o, "script"); err != nil {
		return err
	}
	if s.timeout, err = s.intOption(o, "timeout"); err != nil {
		return err
	}

	return nil
}

// stringOption reads an optional string option. A missing key leaves the field
// at its zero value; a present value of another type is a configuration error.
func (s *Script) stringOption(o map[string]any, keyname string) (string, error) {
	x, ok := o[keyname]
	if !ok {
		return "", nil
	}
	y, ok := x.(string)
	if !ok {
		return "", fmt.Errorf("script: unable to coerce value for '%s': got %T, want string", keyname, x)
	}
	return y, nil
}

// intOption reads an optional int option, with the same missing/typed rules as
// stringOption.
func (s *Script) intOption(o map[string]any, keyname string) (int, error) {
	x, ok := o[keyname]
	if !ok {
		return 0, nil
	}
	y, ok := x.(int)
	if !ok {
		return 0, fmt.Errorf("script: unable to coerce value for '%s': got %T, want int", keyname, x)
	}
	return y, nil
}
