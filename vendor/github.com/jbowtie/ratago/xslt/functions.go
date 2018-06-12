package xslt

import (
	"fmt"
	"github.com/moovweb/gokogiri/xml"
	"github.com/moovweb/gokogiri/xpath"
	"unsafe"
)

func (style *Stylesheet) RegisterXsltFunctions() {
	style.Functions["{}document"] = XsltDocumentFn
	style.Functions["{}generate-id"] = XsltGenerateId
	style.Functions["{}key"] = XsltKey
	style.Functions["{}system-property"] = XsltSystemProperty
	style.Functions["{}unparsed-entity-uri"] = XsltUnparsedEntityUri
	style.Functions["{}current"] = XsltCurrent
	style.Functions["{}element-available"] = XsltElementAvailable
	style.Functions["{}function-available"] = XsltFunctionAvailable
	//format-number - requires handling decimal-format

	// id and lang are built into libxml2, don't need to be registered
}

type Key struct {
	nodes map[string]xml.Nodeset
	use   string
	match string
}

// Implementation of key() from XSLT spec
func XsltKey(context xpath.VariableScope, args []interface{}) interface{} {
	if len(args) < 2 {
		return nil
	}
	// always convert to string
	name := args[0].(string)
	// convert to string (TODO: unless nodeset)
	val := ""
	switch v := args[1].(type) {
	case string:
		val = v
	case []unsafe.Pointer:
		// nodeset; see xsl:key spec for how to handle this
	}
	//get the execution context
	c := context.(*ExecutionContext)
	//look up the key
	k, ok := c.Style.Keys[name]
	if !ok {
		return nil
	}
	result, _ := k.nodes[val]
	//return the nodeset
	return result.ToPointers()
}

// Implementation of system-property() from XSLT spec
func XsltSystemProperty(context xpath.VariableScope, args []interface{}) interface{} {
	if len(args) < 1 {
		return nil
	}
	switch args[0].(string) {
	case "xsl:version":
		return 1.0
	case "xsl:vendor":
		return "John C Barstow"
	case "xsl:vendor-url":
		return "http://github.com/jbowtie/ratago"
	default:
		fmt.Println("EXEC system-property", args[0])
	}
	return nil
}

//Implementation of document() from XSLT spec
func XsltDocumentFn(context xpath.VariableScope, args []interface{}) interface{} {
	if len(args) < 1 {
		return nil
	}
	c := context.(*ExecutionContext)

	switch doc := args[0].(type) {
	case string:
		if doc == "" {
			nodeset := xml.Nodeset{c.Style.Doc}
			return nodeset.ToPointers()
		}
		input := c.FetchInputDocument(doc, false)
		if input != nil {
			nodeset := xml.Nodeset{input}
			return nodeset.ToPointers()
		}
		return nil
	case []unsafe.Pointer:
		n := xml.NewNode(doc[0], nil)
		location := n.Content()
		input := c.FetchInputDocument(location, true)
		if input != nil {
			nodeset := xml.Nodeset{input}
			return nodeset.ToPointers()
		}
		fmt.Println("DOCUMENT", location)
	}
	return nil
}

// Implementation of generate-id() from XSLT spec
func XsltGenerateId(context xpath.VariableScope, args []interface{}) interface{} {
	// should be 0 or 1 argument
	if len(args) > 1 {
		return nil
	}

	//c := context.(*ExecutionContext)
	if len(args) < 1 {
		fmt.Println("GENERATE-ID for current")
		return "N" //id of context node
	}

	switch v := args[0].(type) {
	case []unsafe.Pointer:
		if len(v) == 0 {
			return nil
		}
		out := fmt.Sprintf("N%v", uintptr(v[0]))
		return out
	default:
		return nil
	}
	return nil
}

// Implementation of unparsed-entity-uri() from XSLT spec
func XsltUnparsedEntityUri(context xpath.VariableScope, args []interface{}) interface{} {
	if len(args) < 1 {
		return nil
	}
	c := context.(*ExecutionContext)
	name := argValToString(args[0])
	val := c.Source.UnparsedEntityURI(name)
	return val
}

// Implementation of current() from XSLT spec
func XsltCurrent(context xpath.VariableScope, args []interface{}) interface{} {
	c := context.(*ExecutionContext)
	n := xml.Nodeset{c.Current}
	return n.ToPointers()
}

// Implementation of function-available() from XSLT spec
func XsltFunctionAvailable(context xpath.VariableScope, args []interface{}) interface{} {
	if len(args) < 1 {
		return nil
	}
	c := context.(*ExecutionContext)
	qname := args[0].(string)
	//TODO: resolve namespace of argument
	return c.IsFunctionRegistered("", qname)
}

// Implementation of element-available() from XSLT spec
func XsltElementAvailable(context xpath.VariableScope, args []interface{}) interface{} {
	return false
}

// util function because we can't assume we're actually getting a string
func argValToString(val interface{}) (out string) {
	if val == nil {
		return
	}
	switch v := val.(type) {
	case string:
		return v
	case []unsafe.Pointer:
		if len(v) == 0 {
			return
		}
		n := xml.NewNode(v[0], nil)
		out = n.Content()
	default:
		out = fmt.Sprintf("%v", v)
	}
	return
}
