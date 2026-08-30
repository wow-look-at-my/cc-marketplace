package main

import (
	"encoding/xml"
	"strings"
)

// XML parsing types

type xmlTest struct {
	Command  string `xml:"cmd,attr"`
	Expected string `xml:"expect,attr"`
}

// Several sections, a single rule type: the section supplies the verdict, so
// the identical node denies under <deny> and allows under <allow>.
type xmlRules struct {
	XMLName    xml.Name       `xml:"rules"`
	Tests      []xmlTest      `xml:"test"`
	Allow      xmlSection     `xml:"allow"`
	Ask        xmlSection     `xml:"ask"`
	Deny       xmlSection     `xml:"deny"`
	MCPServers []xmlMCPServer `xml:"mcpServer"`
}

type xmlSection struct {
	Rules []xmlCommand `xml:"rule"`
}

type xmlMCPServer struct {
	Name  string   `xml:"name,attr"`
	Tools []string `xml:"tool"`
}

type xmlCommand struct {
	Name        string `xml:"name,attr"`
	Description string `xml:"description,attr,omitempty"`

	// Matches the RESOLVED process name, not the command spelling.
	Process string `xml:"process,attr,omitempty"`
	InlineScript    bool   `xml:"inlineScript,attr,omitempty"`
	EvalFlags       string `xml:"evalFlags,attr,omitempty"`
	EvalSubcommands string `xml:"evalSubcommands,attr,omitempty"`
	Message         string `xml:"message,attr,omitempty"`

	AllowedFlagsAttr   string          `xml:"allowedFlags,attr,omitempty"`
	DenyWithMessage    string          `xml:"denyWithMessage,attr,omitempty"`
	HelpAlwaysAllowed  bool            `xml:"helpAlwaysAllowed,attr,omitempty"`
	BareOnly           bool            `xml:"bareOnly,attr,omitempty"`
	Tests              []xmlTest       `xml:"test"`
	AllowedFlags       *xmlFlagList    `xml:"allowedFlags"`
	DeniedFlags        *xmlFlagList    `xml:"deniedFlags"`
	ExecFlags          *xmlFlagList    `xml:"execFlags"`
	RequiredFlags      *xmlFlagList    `xml:"requiredFlags"`
	FlagsWithValue     *xmlFlagList    `xml:"flagsWithValue"`
	DenyArgSubstrings  *xmlStringList  `xml:"denyArgSubstrings"`
	AllowedArgPrefixes *xmlStringList  `xml:"allowedArgPrefixes"`
	RequireFlagValue   *xmlRequireFlag `xml:"requireFlagValue"`
	Subcommands        []xmlCommand    `xml:"rule"`
}

type xmlFlagList struct {
	Flags []xmlFlag `xml:"flag"`
}

type xmlFlag struct {
	Name string `xml:"name,attr"`
}

type xmlStringList struct {
	Values []string `xml:"value"`
}

type xmlRequireFlag struct {
	Default string    `xml:"default,attr"`
	Flags   []xmlFlag `xml:"flag"`
	Allowed []string  `xml:"allowed"`
}

func loadXMLRules(data []byte) (Rules, error) {
	var xr xmlRules
	if err := xml.Unmarshal(data, &xr); err != nil {
		return Rules{}, err
	}
	var r Rules
	// Command rules keep their per-section list; process rules are collected
	// separately because they are answered by walking the parse tree, not by
	// matching argv.
	for _, section := range []struct {
		rules    []xmlCommand
		commands *[]CommandNode
		procs    *[]ProcessRule
		behavior string
	}{
		{xr.Allow.Rules, &r.Allow, &r.AllowProcesses, "allow"},
		{xr.Ask.Rules, &r.Ask, &r.AskProcesses, "ask"},
		{xr.Deny.Rules, &r.Deny, &r.DenyProcesses, "deny"},
	} {
		for _, xc := range section.rules {
			if xc.Process != "" {
				*section.procs = append(*section.procs, convertXMLProcess(xc, section.behavior))
				continue
			}
			*section.commands = append(*section.commands, convertXMLCommand(xc))
		}
	}
	if len(xr.MCPServers) > 0 {
		r.MCPServers = make(map[string][]string, len(xr.MCPServers))
		for _, s := range xr.MCPServers {
			r.MCPServers[s.Name] = s.Tools
		}
	}
	return r, nil
}

func convertXMLProcess(xc xmlCommand, behavior string) ProcessRule {
	return ProcessRule{
		Name:            xc.Process,
		Behavior:        behavior,
		Message:         xc.Message,
		InlineOnly:      xc.InlineScript,
		EvalFlags:       splitList(xc.EvalFlags),
		EvalSubcommands: splitList(xc.EvalSubcommands),
	}
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func convertXMLCommand(xc xmlCommand) CommandNode {
	node := CommandNode{
		Description:       xc.Description,
		DenyWithMessage:   xc.DenyWithMessage,
		HelpAlwaysAllowed: xc.HelpAlwaysAllowed,
		BareOnly:          xc.BareOnly,
	}

	names := strings.Split(xc.Name, ",")
	if len(names) == 1 {
		node.Name = names[0]
	} else {
		ifaces := make([]interface{}, len(names))
		for i, n := range names {
			ifaces[i] = strings.TrimSpace(n)
		}
		node.Name = ifaces
	}

	if xc.AllowedFlagsAttr == "*" {
		node.AllowedFlags = "*"
	} else if xc.AllowedFlags != nil {
		flags := make([]interface{}, len(xc.AllowedFlags.Flags))
		for i, f := range xc.AllowedFlags.Flags {
			flags[i] = f.Name
		}
		node.AllowedFlags = flags
	}

	node.DeniedFlags = xmlFlagNames(xc.DeniedFlags)
	node.ExecFlags = xmlFlagNames(xc.ExecFlags)
	node.RequiredFlags = xmlFlagNames(xc.RequiredFlags)
	node.FlagsWithValue = xmlFlagNames(xc.FlagsWithValue)

	if xc.DenyArgSubstrings != nil {
		node.DenyArgSubstrings = xc.DenyArgSubstrings.Values
	}

	if xc.AllowedArgPrefixes != nil {
		node.AllowedArgPrefixes = xc.AllowedArgPrefixes.Values
	}

	if xc.RequireFlagValue != nil {
		rfv := &RequireFlagRule{
			Default: xc.RequireFlagValue.Default,
			Allowed: xc.RequireFlagValue.Allowed,
		}
		for _, f := range xc.RequireFlagValue.Flags {
			rfv.Flags = append(rfv.Flags, f.Name)
		}
		node.RequireFlagValue = rfv
	}

	for _, xs := range xc.Subcommands {
		node.Subcommands = append(node.Subcommands, convertXMLCommand(xs))
	}

	return node
}

func xmlFlagNames(fl *xmlFlagList) []string {
	if fl == nil {
		return nil
	}
	names := make([]string, len(fl.Flags))
	for i, f := range fl.Flags {
		names[i] = f.Name
	}
	return names
}
