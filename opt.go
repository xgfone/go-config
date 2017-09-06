package configmanager

import "fmt"

type optType int

func (ot optType) String() string {
	return optTypeMap[ot]
}

const (
	noneType optType = iota
	stringType
	intType
	int8Type
	int16Type
	int32Type
	int64Type
	uintType
	uint8Type
	uint16Type
	uint32Type
	uint64Type
	float32Type
	float64Type

	stringsType
	intsType
	int64sType
	uintsType
	uint64sType
)

var optTypeMap = map[optType]string{
	noneType:    "none",
	stringType:  "string",
	intType:     "int",
	int8Type:    "int8",
	int16Type:   "int16",
	int32Type:   "int32",
	int64Type:   "int64",
	uintType:    "uint",
	uint8Type:   "uint8",
	uint16Type:  "uint16",
	uint32Type:  "uint32",
	uint64Type:  "uint64",
	float32Type: "float32",
	float64Type: "float64",

	stringsType: "[]string",
	intsType:    "[]int",
	int64sType:  "[]int64",
	uintsType:   "[]uint",
	uint64sType: "[]uint64",
}

type baseOpt struct {
	Name     string
	Help     string
	Short    string
	Required bool
	Default  interface{}

	_type optType
}

func newBaseOpt(short, name string, _default interface{}, required bool,
	help string, optType optType) baseOpt {
	o := baseOpt{
		Short:    short,
		Name:     name,
		Help:     help,
		Required: required,
		Default:  _default,
		_type:    optType,
	}
	o.GetDefault()
	return o
}

// GetName returns the name of the option.
func (o baseOpt) GetName() string {
	return o.Name
}

// GetShort returns the shorthand name of the option.
func (o baseOpt) GetShort() string {
	return o.Short
}

// IsRequired returns ture if the option must have the value, or false.
func (o baseOpt) IsRequired() bool {
	return o.Required
}

// GetHelp returns the help doc of the option.
func (o baseOpt) GetHelp() string {
	return o.Help
}

// GetDefault returns the default value of the option.
func (o baseOpt) GetDefault() interface{} {
	if o.Default == nil {
		return nil
	}

	switch o._type {
	case stringType:
		return o.Default.(string)
	case intType:
		return o.Default.(int)
	default:
		panic(fmt.Errorf("don't support the type '%s'", o._type))
	}
}

// Parse parses the value of the option to a certain type.
func (o baseOpt) Parse(data string) (interface{}, error) {
	switch o._type {
	case stringType:
		return ToString(data)
	case intType:
		_v, err := ToInt64(data)
		if err != nil {
			return nil, err
		}
		return int(_v), nil
	default:
		panic(fmt.Errorf("don't support the type '%s'", o._type))
	}
}

// strOpt is a string option
type strOpt struct {
	baseOpt
}

var _ Opt = strOpt{}

// NewStrOpt return a new string option.
//
// Notice: the type of the default value must be string or nil.
// If no default, it's nil.
func NewStrOpt(short, name string, _default interface{}, required bool, help string) Opt {
	return strOpt{newBaseOpt(short, name, _default, required, help, stringType)}
}

// intOpt is a int option
type intOpt struct {
	baseOpt
}

var _ Opt = intOpt{}

// NewIntOpt return a new int option.
//
// Notice: the type of the default value must be int or nil.
// If no default, it's nil.
func NewIntOpt(short, name string, _default interface{}, required bool, help string) Opt {
	return intOpt{newBaseOpt(short, name, _default, required, help, stringType)}
}
