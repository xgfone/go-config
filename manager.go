/*
Copyright 2017 xgfone

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package config is an extensible go configuration manager.
//
// The default parsers can parse the CLI and ENV arguments and the ini and property file. You can
// implement and register your parser, and the configuration engine will call
// the parser to parse the configuration.
//
// The inspiration is from [oslo.config](https://github.com/openstack/oslo.config),
// which is a OpenStack library for config.
package config

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

var (
	// ErrParsed is an error that the config has been parsed.
	ErrParsed = fmt.Errorf("the config manager has been parsed")

	// ErrNotParsed is an error that the config has not been parsed.
	ErrNotParsed = fmt.Errorf("the config manager has not been parsed")
)

// StructValidator is used to validate the struct value.
type StructValidator interface {
	Validate() error
}

// Config is used to manage the configuration parsers.
type Config struct {
	parsed bool

	isRequired bool
	isDebug    bool
	isPanic    bool
	isZero     bool

	vName    string
	vHelp    string
	vVersion string

	args    []string
	cliArgs []string
	parsers []Parser

	groupSep    string
	groupName   string // Default Group Name
	groupPrefix string // The prefix of the default group name.

	watch      func(string, string, interface{})
	groups     map[string]*OptGroup
	validators []func() error
}

// NewConfig returns a new Config.
//
// The name of the default group is DEFAULT.
func NewConfig() *Config {
	conf := &Config{
		isZero:     true,
		isPanic:    true,
		isRequired: true,
		groupName:  DefaultGroupName,
		groups:     make(map[string]*OptGroup, 2),
	}
	return conf.SetGroupSeparator(".")
}

func (c *Config) debug(format string, args ...interface{}) {
	if c.isDebug {
		fmt.Printf(format+"\n", args...)
	}
}

// Printf prints the message to os.Stdout if enabling debug.
func (c *Config) Printf(format string, args ...interface{}) {
	c.debug(format, args...)
}

//////////////////////////////////////////////////////////////////////////////
/// Manage Metadata

// SetDefaultGroupName resets the name of the default group.
//
// If you want to modify it, you must do it before registering any options.
//
// If parsed, it will panic when calling it.
func (c *Config) SetDefaultGroupName(name string) *Config {
	c.panicIsParsed(true)
	c.groupName = name
	c.groupPrefix = c.groupName + c.groupSep
	return c
}

// GetDefaultGroupName returns the name of the default group.
func (c *Config) GetDefaultGroupName() string {
	return c.groupName
}

// SetRequired asks that all the registered options have a value.
//
// Notice: the nil value is not considered that there is a value, but the ZERO
// value is that.
//
// If parsed, it will panic when calling it.
func (c *Config) SetRequired(required bool) *Config {
	c.panicIsParsed(true)
	c.isRequired = required
	return c
}

// IgnoreReregister decides whether it will panic when reregistering an option
// into a certain group.
//
// The default is not to ignore it, but you can set it to false to ignore it.
func (c *Config) IgnoreReregister(ignore bool) *Config {
	c.panicIsParsed(true)
	c.isPanic = !ignore
	return c
}

// SetZero sets the value of the option to the zero value of its type
// if the option has no value.
//
// If parsed, it will panic when calling it.
func (c *Config) SetZero(zero bool) *Config {
	c.panicIsParsed(true)
	c.isZero = zero
	return c
}

// SetDebug enables the debug model.
//
// If setting, when registering the option, it'll output the verbose information.
// You should set it before registering the option.
//
// If parsed, it will panic when calling it.
func (c *Config) SetDebug(debug bool) *Config {
	c.panicIsParsed(true)
	c.isDebug = debug
	return c
}

// IsDebug returns whether the config manager is on the debug mode.
func (c *Config) IsDebug() bool {
	return c.isDebug
}

// SetGroupSeparator sets the separator between the group names.
//
// The default separator is a dot(.).
//
// If parsed, it will panic when calling it.
func (c *Config) SetGroupSeparator(sep string) *Config {
	if sep == "" {
		panic(fmt.Errorf("the separator is empty"))
	}

	c.panicIsParsed(true)
	c.groupSep = sep
	c.groupPrefix = c.groupName + c.groupSep
	return c
}

// GetGroupSeparator returns the group separator.
func (c *Config) GetGroupSeparator() string {
	return c.groupSep
}

//////////////////////////////////////////////////////////////////////////////
/// Start To Parse

func (c *Config) panicIsParsed(p bool) {
	if p && c.parsed {
		panic(ErrParsed)
	}
	if !p && !c.parsed {
		panic(ErrNotParsed)
	}
}

// Parsed returns true if has been parsed, or false.
func (c *Config) Parsed() bool {
	return c.parsed
}

// Parse parses the option, including CLI, the config file, or others.
//
// if the arguments is nil, it's equal to os.Args[1:].
//
// After parsing a certain option, it will call the validators of the option
// to validate whether the option value is valid.
//
// If parsed, it will panic when calling it.
func (c *Config) Parse(args ...string) (err error) {
	c.panicIsParsed(true)
	c.getGroupByName(c.groupName, true) // Ensure that the default group exists.

	if args == nil {
		c.cliArgs = os.Args[1:]
	} else {
		c.cliArgs = args
	}

	for _, parser := range c.parsers {
		c.debug("Initializing the parser '%s'", parser.Name())
		if err = parser.Pre(c); err != nil {
			return err
		}
	}

	c.parsed = true
	for _, parser := range c.parsers {
		c.debug("Calling the parser '%s'", parser.Name())
		if err = parser.Parse(c); err != nil {
			return fmt.Errorf("The '%s' parser failed: %s", parser.Name(), err)
		}
	}

	for _, parser := range c.parsers {
		c.debug("Cleaning the parser '%s'", parser.Name())
		if err = parser.Post(c); err != nil {
			return err
		}
	}

	// Check whether all the groups have parsed all the required options.
	for _, group := range c.groups {
		if err = group.checkRequiredOption(); err != nil {
			return err
		}
	}

	for _, v := range c.validators {
		if err = v(); err != nil {
			return err
		}
	}

	return
}

//////////////////////////////////////////////////////////////////////////////
/// Manage Parsers

// SetVersion sets the version information.
//
// If the CLI parser support the version function, it will print the version
// and exit when giving the CLI option version.
//
// It supports:
//     SetVersion(version)             // SetVersion("1.0.0")
//     SetVersion(version, name)       // SetVersion("1.0.0", "version")
//     SetVersion(version, name, help) // SetVersion("1.0.0", "version", "Print the version")
//
// Notice: it is for the CLI parser.
func (c *Config) SetVersion(version string, args ...string) *Config {
	name := "version"
	help := "Print the version and exit."
	if len(args) == 1 {
		name = args[0]
	} else if len(args) > 1 {
		name = args[0]
		help = args[1]
	}

	if name == "" || version == "" || help == "" {
		panic(fmt.Errorf("The arguments about version must not be empty"))
	}

	c.vName = name
	c.vHelp = help
	c.vVersion = version
	return c
}

// GetVersion returns the information about version.
//
// Notice: it is for the CLI parser.
func (c *Config) GetVersion() (name, version, help string) {
	return c.vName, c.vVersion, c.vHelp
}

// CliArgs returns the parsed cil argments.
func (c *Config) CliArgs() []string {
	return c.cliArgs
}

// Args returns the rest of the CLI arguments, which are not the options
// starting with the prefix "-", "--" or others, etc.
//
// Notice: you should not modify the returned string slice result.
//
// If not parsed, it will panic when calling it.
func (c *Config) Args() []string {
	c.panicIsParsed(false)
	return c.args
}

// SetArgs sets the cli rest arguments.
//
// Notice: it will be called by the cli parser when parsing cli arguments.
func (c *Config) SetArgs(args []string) {
	c.args = args
}

func (c *Config) sortParsers() {
	sort.SliceStable(c.parsers, func(i, j int) bool {
		return c.parsers[i].Priority() < c.parsers[j].Priority()
	})
}

// AddParser adds a few parsers.
func (c *Config) AddParser(parsers ...Parser) *Config {
	c.panicIsParsed(true)
	c.parsers = append(c.parsers, parsers...)
	c.sortParsers()

	buf := bytes.NewBufferString("After adding the parser: [")
	for i, p := range c.parsers {
		if i > 0 {
			fmt.Fprintf(buf, ", %s(%d)", p.Name(), p.Priority())
		} else {
			fmt.Fprintf(buf, "%s(%d)", p.Name(), p.Priority())
		}
	}
	buf.WriteString("]")
	c.debug(buf.String())
	return c
}

// RemoveParser removes and returns the parser named name.
//
// Return nil if the parser does not exist.
func (c *Config) RemoveParser(name string) Parser {
	c.panicIsParsed(true)
	for i, p := range c.parsers {
		if p.Name() == name {
			ps := make([]Parser, 0, len(c.parsers)-1)
			ps = append(ps, c.parsers[:i]...)
			ps = append(ps, c.parsers[i:]...)
			c.parsers = ps
			return p
		}
	}
	return nil
}

// GetParser returns the parser named name.
//
// Return nil if the parser does not exist.
func (c *Config) GetParser(name string) Parser {
	for _, p := range c.parsers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// HasParser reports whether the parser named name exists.
func (c *Config) HasParser(name string) bool {
	return c.GetParser(name) != nil
}

///////////////////////////////////////////////////////////////////////////////
/// Register Options

// RegisterStruct registers the field name of the struct as options into the
// group "group".
//
// If the group name is "", it's regarded as the default group. And the struct
// must be a pointer to a struct variable, or it will panic.
//
// If parsed, it will panic when calling it.
//
// The tag of the field supports "name", "short", "default", "help", which are
// equal to the name, the short name, the default, the help of the option.
// If you want to ignore a certain field, just set the tag "name" to "-",
// such as `name:"-"`. The field also contains the tag "cli", whose value maybe
// "1", "t", "T", "on", "On", "ON", "true", "True", "TRUE", and which represents
// the option is also registered into the CLI parser; but you can also use "0",
// "f", "F", "off", "Off", "OFF", "false", "False" or "FALSE" to override or
// disable it. Moreover, you can use the tag "group" to reset the group name,
// that's, the group of the field with the tag "group" is different to the group
// of the whole struct. If the value of the tag "group" is empty, the default
// group will be used in preference.
//
// If the struct has implemented the interface StructValidator, this validator
// will be called automatically after having parsed.
//
// Notice: If having no the tag "name", the name of the option is the lower-case
// of the field name.
//
// Notice: The struct supports the nested struct, but not the pointer field.
//
// Notice: The struct doesn't support the validator. You maybe choose others,
// such as github.com/asaskevich/govalidator.
//
// NOTICE: ALL THE TAGS ARE OPTIONAL.
//
// Notice: For the struct option, you shouldn't call SetOptValue()
// because of concurrence.
func (c *Config) RegisterStruct(group string, s interface{}) {
	c.registerStruct(group, s, false)
}

// RegisterCliStruct is the same as RegisterStruct, but it will register
// the option into the CLI parser by default.
func (c *Config) RegisterCliStruct(group string, s interface{}) {
	c.registerStruct(group, s, true)
}

func (c *Config) registerStruct(group string, s interface{}, cli bool) {
	c.panicIsParsed(true)
	c.getGroupByName(strings.Trim(group, c.groupSep), true).registerStruct(s, cli)
	if v, ok := s.(StructValidator); ok {
		c.validators = append(c.validators, v.Validate)
	}
}

// RegisterCliOpt registers the option into the group.
//
// It registers the option to not only all the common parsers but also the CLI
// parser.
//
// If the group name is "", it's regarded as the default group.
//
// If parsed, it will panic when calling it.
func (c *Config) RegisterCliOpt(group string, opt Opt) {
	c.registerOpt(group, true, opt)
}

// RegisterCliOpts registers the options into the group.
//
// It registers the options to not only all the common parsers but also the CLI
// parser.
//
// If the group name is "", it's regarded as the default group.
//
// If parsed, it will panic when calling it.
func (c *Config) RegisterCliOpts(group string, opts []Opt) {
	for _, opt := range opts {
		c.RegisterCliOpt(group, opt)
	}
}

// RegisterOpt registers the option into the group.
//
// It only registers the option to all the common parsers, not the CLI parser.
//
// If the group name is "", it's regarded as the default group.
//
// If parsed, it will panic when calling it.
func (c *Config) RegisterOpt(group string, opt Opt) {
	c.registerOpt(group, false, opt)
}

// RegisterOpts registers the options into the group.
//
// It only registers the options to all the common parsers, not the CLI parser.
//
// If the group name is "", it's regarded as the default group.
//
// If parsed, it will panic when calling it.
func (c *Config) RegisterOpts(group string, opts []Opt) {
	for _, opt := range opts {
		c.RegisterOpt(group, opt)
	}
}

// registerOpt registers the option into the group.
//
// If the group name is "", it's regarded as the default group.
//
// The first argument, cli, indicates whether the option is as the CLI option,
// too.
//
// If parsed, it will panic when calling it.
func (c *Config) registerOpt(group string, cli bool, opt Opt) {
	c.panicIsParsed(true)
	c.getGroupByName(group, true).registerOpt(cli, opt)
}

//////////////////////////////////////////////////////////////////////////////
/// Set and Observe the option value

// Observe watches the change of values.
//
// When the option value is changed, the function f will be called.
//
// If SetOptValue() is used in the multi-thread, you should promise
// that the callback function f is thread-safe and reenterable.
func (c *Config) Observe(f func(groupName string, optName string, optValue interface{})) {
	c.panicIsParsed(true)
	c.watch = f
}

// SetOptValue sets the value of the option in the group. It's thread-safe.
//
// priority it should be the priority of the parser. It only set the option value
// successfully for the priority higher than the last. So you can use 0
// to update it coercively.
//
// Notice: You cannot call SetOptValue() for the struct option, because we have
// no way to promise that it's thread-safe.
func (c *Config) SetOptValue(priority int, groupName, optName string, optValue interface{}) error {
	if priority < 0 {
		return fmt.Errorf("the priority must not be the negative")
	}

	if group := c.getGroupByName(groupName, false); group != nil {
		return group.setOptValue(priority, optName, optValue)
	}
	return fmt.Errorf("no group '%s'", groupName)
}

///////////////////////////////////////////////////////////////////////////////
/// Manage Group

// PrintGroupTree prints the tree of the groups to os.Stdout.
//
// Notice: it is only used to debug.
func (c *Config) PrintGroupTree() {
	var gnames []string
	for _, g := range c.Groups() {
		gnames = append(gnames, g.Name())
	}
	sort.Strings(gnames)

	tree := make(map[string]interface{}, 8)
	for _, gname := range gnames {
		parent := tree
		for _, name := range strings.Split(gname, c.groupSep) {
			if v, ok := parent[name]; ok {
				parent = v.(map[string]interface{})
			} else {
				m := make(map[string]interface{})
				parent[name] = m
				parent = m
			}
		}
	}

	c.printMap("", tree, "")
}

func (c *Config) printMap(parent string, ms map[string]interface{}, indent string) {
	group := c.Group(parent)
	for gname, m := range ms {
		fmt.Printf("|%s-->[%s]\n", indent, gname)
		for _, opt := range group.Group(gname).AllOpts() {
			fmt.Printf("|%s   |--> %s\n", indent, opt.Name())
		}

		if _ms, ok := m.(map[string]interface{}); ok && len(_ms) > 0 {
			c.printMap(c.mergeGroupName(parent, gname), _ms, indent+"   |")
		}
	}
}

// Groups is the same as AllGroups, except those groups that have no options,
// which are the assistant groups.
func (c *Config) Groups() []*OptGroup {
	// c.panicIsParsed(false)
	groups := make([]*OptGroup, 0, len(c.groups))
	for _, group := range c.groups {
		if len(group.opts) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// AllGroups returns all the groups.
//
// Notice: you should not modify the returned slice result.
func (c *Config) AllGroups() []*OptGroup {
	// c.panicIsParsed(false)
	groups := make([]*OptGroup, 0, len(c.groups))
	for _, group := range c.groups {
		groups = append(groups, group)
	}
	return groups
}

func (c *Config) mergeGroupName(parent, name string) string {
	if parent == "" {
		return name
	}
	return strings.TrimPrefix(parent+"."+name, c.groupPrefix)
}

func (c *Config) getGroupName(name string) string {
	if name == "" {
		return c.groupName
	}
	return name
}

func (c *Config) newOptGroup(name, fullName string) *OptGroup {
	group := c.groups[name]
	if group == nil {
		group = newOptGroup(name, fullName, c)
		c.groups[name] = group
		c.debug("Creating group '%s'", name)
	}
	return group
}

func (c *Config) getGroupByName(name string, new bool) *OptGroup {
	name = strings.TrimPrefix(name, c.groupPrefix)

	if !new {
		return c.groups[c.getGroupName(name)]
	} else if name == "" {
		return c.newOptGroup(c.groupName, c.groupName)
	}

	groups := strings.Split(name, c.groupSep)
	for i, gname := range groups {
		fullName := strings.Join(groups[:i+1], c.groupSep)
		c.newOptGroup(fullName, fullName)
		c.newOptGroup(gname, fullName)
	}

	return c.groups[name]
}

// NewGroup news and returns a group named group.
func (c *Config) NewGroup(group string) *OptGroup {
	c.panicIsParsed(true)
	return c.getGroupByName(group, true)
}

// HasGroup reports whether there is the group named 'group'.
func (c *Config) HasGroup(group string) bool {
	// c.panicIsParsed(false)
	return c.getGroupByName(group, false) != nil
}

// Group returns the OptGroup named group.
//
// Return the default group if the group name is "".
//
// The group must exist, or panic.
func (c *Config) Group(group string) *OptGroup {
	// c.panicIsParsed(false)
	if g := c.getGroupByName(group, false); g != nil {
		return g
	}
	panic(fmt.Errorf("have no group '%s'", group))
}

// G is the short for c.Group(group).
func (c *Config) G(group string) *OptGroup {
	return c.Group(group)
}

//////////////////////////////////////////////////////////////////////////////
/// Get the value from the group.

// Value is equal to c.Group("").Value(name).
func (c *Config) Value(name string) interface{} {
	return c.Group("").Value(name)
}

// V is the short for c.Value(name).
func (c *Config) V(name string) interface{} {
	return c.Value(name)
}

// BoolE is equal to c.Group("").BoolE(name).
func (c *Config) BoolE(name string) (bool, error) {
	return c.Group("").BoolE(name)
}

// BoolD is equal to c.Group("").BoolD(name, _default).
func (c *Config) BoolD(name string, _default bool) bool {
	return c.Group("").BoolD(name, _default)
}

// Bool is equal to c.Group("").Bool(name).
func (c *Config) Bool(name string) bool {
	return c.Group("").Bool(name)
}

// StringE is equal to c.Group("").StringE(name).
func (c *Config) StringE(name string) (string, error) {
	return c.Group("").StringE(name)
}

// StringD is equal to c.Group("").StringD(name, _default).
func (c *Config) StringD(name, _default string) string {
	return c.Group("").StringD(name, _default)
}

// String is equal to c.Group("").String(name).
func (c *Config) String(name string) string {
	return c.Group("").String(name)
}

// IntE is equal to c.Group("").IntE(name).
func (c *Config) IntE(name string) (int, error) {
	return c.Group("").IntE(name)
}

// IntD is equal to c.Group("").IntD(name, _default).
func (c *Config) IntD(name string, _default int) int {
	return c.Group("").IntD(name, _default)
}

// Int is equal to c.Group("").Int(name).
func (c *Config) Int(name string) int {
	return c.Group("").Int(name)
}

// Int8E is equal to c.Group("").Int8E(name).
func (c *Config) Int8E(name string) (int8, error) {
	return c.Group("").Int8E(name)
}

// Int8D is equal to c.Group("").Int8D(name, _default).
func (c *Config) Int8D(name string, _default int8) int8 {
	return c.Group("").Int8D(name, _default)
}

// Int8 is equal to c.Group("").Int8(name).
func (c *Config) Int8(name string) int8 {
	return c.Group("").Int8(name)
}

// Int16E is equal to c.Group("").Int16E(name).
func (c *Config) Int16E(name string) (int16, error) {
	return c.Group("").Int16E(name)
}

// Int16D is equal to c.Group("").Int16D(name, _default).
func (c *Config) Int16D(name string, _default int16) int16 {
	return c.Group("").Int16D(name, _default)
}

// Int16 is equal to c.Group("").Int16(name).
func (c *Config) Int16(name string) int16 {
	return c.Group("").Int16(name)
}

// Int32E is equal to c.Group("").Int32E(name).
func (c *Config) Int32E(name string) (int32, error) {
	return c.Group("").Int32E(name)
}

// Int32D is equal to c.Group("").Int32D(name, _default).
func (c *Config) Int32D(name string, _default int32) int32 {
	return c.Group("").Int32D(name, _default)
}

// Int32 is equal to c.Group("").Int32(name).
func (c *Config) Int32(name string) int32 {
	return c.Group("").Int32(name)
}

// Int64E is equal to c.Group("").Int64E(name).
func (c *Config) Int64E(name string) (int64, error) {
	return c.Group("").Int64E(name)
}

// Int64D is equal to c.Group("").Int64D(name, _default).
func (c *Config) Int64D(name string, _default int64) int64 {
	return c.Group("").Int64D(name, _default)
}

// Int64 is equal to c.Group("").Int64(name).
func (c *Config) Int64(name string) int64 {
	return c.Group("").Int64(name)
}

// UintE is equal to c.Group("").UintE(name).
func (c *Config) UintE(name string) (uint, error) {
	return c.Group("").UintE(name)
}

// UintD is equal to c.Group("").UintD(name, _default).
func (c *Config) UintD(name string, _default uint) uint {
	return c.Group("").UintD(name, _default)
}

// Uint is equal to c.Group("").Uint(name).
func (c *Config) Uint(name string) uint {
	return c.Group("").Uint(name)
}

// Uint8E is equal to c.Group("").Uint8E(name).
func (c *Config) Uint8E(name string) (uint8, error) {
	return c.Group("").Uint8E(name)
}

// Uint8D is equal to c.Group("").Uint8D(name, _default).
func (c *Config) Uint8D(name string, _default uint8) uint8 {
	return c.Group("").Uint8D(name, _default)
}

// Uint8 is equal to c.Group("").Uint8(name).
func (c *Config) Uint8(name string) uint8 {
	return c.Group("").Uint8(name)
}

// Uint16E is equal to c.Group("").Uint16E(name).
func (c *Config) Uint16E(name string) (uint16, error) {
	return c.Group("").Uint16E(name)
}

// Uint16D is equal to c.Group("").Uint16D(name, _default).
func (c *Config) Uint16D(name string, _default uint16) uint16 {
	return c.Group("").Uint16D(name, _default)
}

// Uint16 is equal to c.Group("").Uint16(name).
func (c *Config) Uint16(name string) uint16 {
	return c.Group("").Uint16(name)
}

// Uint32E is equal to c.Group("").Uint32E(name).
func (c *Config) Uint32E(name string) (uint32, error) {
	return c.Group("").Uint32E(name)
}

// Uint32D is equal to c.Group("").Uint32D(name, _default).
func (c *Config) Uint32D(name string, _default uint32) uint32 {
	return c.Group("").Uint32D(name, _default)
}

// Uint32 is equal to c.Group("").Uint32(name).
func (c *Config) Uint32(name string) uint32 {
	return c.Group("").Uint32(name)
}

// Uint64E is equal to c.Group("").Uint64E(name).
func (c *Config) Uint64E(name string) (uint64, error) {
	return c.Group("").Uint64E(name)
}

// Uint64D is equal to c.Group("").Uint64D(name, _default).
func (c *Config) Uint64D(name string, _default uint64) uint64 {
	return c.Group("").Uint64D(name, _default)
}

// Uint64 is equal to c.Group("").Uint64(name).
func (c *Config) Uint64(name string) uint64 {
	return c.Group("").Uint64(name)
}

// Float32E is equal to c.Group("").Float32E(name).
func (c *Config) Float32E(name string) (float32, error) {
	return c.Group("").Float32E(name)
}

// Float32D is equal to c.Group("").Float32D(name, _default).
func (c *Config) Float32D(name string, _default float32) float32 {
	return c.Group("").Float32D(name, _default)
}

// Float32 is equal to c.Group("").Float32(name).
func (c *Config) Float32(name string) float32 {
	return c.Group("").Float32(name)
}

// Float64E is equal to c.Group("").Float64E(name).
func (c *Config) Float64E(name string) (float64, error) {
	return c.Group("").Float64E(name)
}

// Float64D is equal to c.Group("").Float64D(name, _default).
func (c *Config) Float64D(name string, _default float64) float64 {
	return c.Group("").Float64D(name, _default)
}

// Float64 is equal to c.Group("").Float64(name).
func (c *Config) Float64(name string) float64 {
	return c.Group("").Float64(name)
}

// DurationE is equal to c.Group("").DurationE(name).
func (c *Config) DurationE(name string) (time.Duration, error) {
	return c.Group("").DurationE(name)
}

// DurationD is equal to c.Group("").DurationD(name, _default).
func (c *Config) DurationD(name string, _default time.Duration) time.Duration {
	return c.Group("").DurationD(name, _default)
}

// Duration is equal to c.Group("").Duration(name).
func (c *Config) Duration(name string) time.Duration {
	return c.Group("").Duration(name)
}

// TimeE is equal to c.Group("").DTimeE(name).
func (c *Config) TimeE(name string) (time.Time, error) {
	return c.Group("").TimeE(name)
}

// TimeD is equal to c.Group("").TimeD(name, _default).
func (c *Config) TimeD(name string, _default time.Time) time.Time {
	return c.Group("").TimeD(name, _default)
}

// Time is equal to c.Group("").Time(name).
func (c *Config) Time(name string) time.Time {
	return c.Group("").Time(name)
}

// StringsE is equal to c.Group("").StringsE(name).
func (c *Config) StringsE(name string) ([]string, error) {
	return c.Group("").StringsE(name)
}

// StringsD is equal to c.Group("").StringsD(name, _default).
func (c *Config) StringsD(name string, _default []string) []string {
	return c.Group("").StringsD(name, _default)
}

// Strings is equal to c.Group("").Strings(name).
func (c *Config) Strings(name string) []string {
	return c.Group("").Strings(name)
}

// IntsE is equal to c.Group("").IntsE(name).
func (c *Config) IntsE(name string) ([]int, error) {
	return c.Group("").IntsE(name)
}

// IntsD is equal to c.Group("").IntsD(name, _default).
func (c *Config) IntsD(name string, _default []int) []int {
	return c.Group("").IntsD(name, _default)
}

// Ints is equal to c.Group("").Ints(name).
func (c *Config) Ints(name string) []int {
	return c.Group("").Ints(name)
}

// Int64sE is equal to c.Group("").Int64sE(name).
func (c *Config) Int64sE(name string) ([]int64, error) {
	return c.Group("").Int64sE(name)
}

// Int64sD is equal to c.Group("").Int64sD(name, _default).
func (c *Config) Int64sD(name string, _default []int64) []int64 {
	return c.Group("").Int64sD(name, _default)
}

// Int64s is equal to c.Group("").Int64s(name).
func (c *Config) Int64s(name string) []int64 {
	return c.Group("").Int64s(name)
}

// UintsE is equal to c.Group("").UintsE(name).
func (c *Config) UintsE(name string) ([]uint, error) {
	return c.Group("").UintsE(name)
}

// UintsD is equal to c.Group("").UintsD(name, _default).
func (c *Config) UintsD(name string, _default []uint) []uint {
	return c.Group("").UintsD(name, _default)
}

// Uints is equal to c.Group("").Uints(name).
func (c *Config) Uints(name string) []uint {
	return c.Group("").Uints(name)
}

// Uint64sE is equal to c.Group("").Uint64sE(name).
func (c *Config) Uint64sE(name string) ([]uint64, error) {
	return c.Group("").Uint64sE(name)
}

// Uint64sD is equal to c.Group("").Uint64sD(name, _default).
func (c *Config) Uint64sD(name string, _default []uint64) []uint64 {
	return c.Group("").Uint64sD(name, _default)
}

// Uint64s is equal to c.Group("").Uint64s(name).
func (c *Config) Uint64s(name string) []uint64 {
	return c.Group("").Uint64s(name)
}

// Float64sE is equal to c.Group("").Float64sE(name).
func (c *Config) Float64sE(name string) ([]float64, error) {
	return c.Group("").Float64sE(name)
}

// Float64sD is equal to c.Group("").Float64sD(name, _default).
func (c *Config) Float64sD(name string, _default []float64) []float64 {
	return c.Group("").Float64sD(name, _default)
}

// Float64s is equal to c.Group("").Float64s(name).
func (c *Config) Float64s(name string) []float64 {
	return c.Group("").Float64s(name)
}

// DurationsE is equal to c.Group("").DurationsE(name).
func (c *Config) DurationsE(name string) ([]time.Duration, error) {
	return c.Group("").DurationsE(name)
}

// DurationsD is equal to c.Group("").DurationsD(name, _default).
func (c *Config) DurationsD(name string, _default []time.Duration) []time.Duration {
	return c.Group("").DurationsD(name, _default)
}

// Durations is equal to c.Group("").Durations(name).
func (c *Config) Durations(name string) []time.Duration {
	return c.Group("").Durations(name)
}

// TimesE is equal to c.Group("").DTimesE(name).
func (c *Config) TimesE(name string) ([]time.Time, error) {
	return c.Group("").TimesE(name)
}

// TimesD is equal to c.Group("").TimesD(name, _default).
func (c *Config) TimesD(name string, _default []time.Time) []time.Time {
	return c.Group("").TimesD(name, _default)
}

// Times is equal to c.Group("").Times(name).
func (c *Config) Times(name string) []time.Time {
	return c.Group("").Times(name)
}
