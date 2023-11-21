// Copyright 2023 The builder-gen Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package generators

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"k8s.io/gengo/args"
	"k8s.io/gengo/examples/set-gen/sets"
	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"
	"k8s.io/klog/v2"
)

// This is the comment tag that carries parameters for deep-copy generation.
const (
	tagEnabledName              = "builder-gen"
	ignoreTagName               = tagEnabledName + ":ignore"
	newMethodCallTagName        = tagEnabledName + ":new-call"
	embeddedIgnoreMethodTagName = tagEnabledName + ":embedded-ignore-method"
)

func extractIgnoreTag(t *types.Type) bool {
	comments := append(append([]string{}, t.SecondClosestCommentLines...), t.CommentLines...)
	values := types.ExtractCommentTags("+", comments)[ignoreTagName]
	if len(values) > 0 {
		return values[0] == "true"
	}
	return false
}

func extractNewMethodCallTag(t *types.Type) []string {
	return extractTag(t, newMethodCallTagName)
}

func extractEmbbedIgnoreMethodTag(t *types.Type) []string {
	return extractTag(t, embeddedIgnoreMethodTagName)
}

func extractTag(t *types.Type, tagName string) []string {
	var result []string
	comments := append(append([]string{}, t.SecondClosestCommentLines...), t.CommentLines...)
	values := types.ExtractCommentTags("+", comments)[tagName]
	for _, v := range values {
		if len(v) == 0 {
			continue
		}
		intfs := strings.Split(v, ",")
		for _, intf := range intfs {
			if intf == "" {
				continue
			}
			result = append(result, intf)
		}
	}
	return result
}

// TODO: This is created only to reduce number of changes in a single PR.
// Remove it and use PublicNamer instead.
func deepCopyNamer() *namer.NameStrategy {
	return &namer.NameStrategy{
		Join: func(pre string, in []string, post string) string {
			return strings.Join(in, "_")
		},
		PrependPackageNames: 1,
	}
}

// NameSystems returns the name system used by the generators in this package.
func NameSystems() namer.NameSystems {
	return namer.NameSystems{
		"public": namer.NewPublicNamer(1),
		"raw":    namer.NewRawNamer("", nil),
	}
}

// DefaultNameSystem returns the default name system for ordering the types to be
// processed by the generators in this package.
func DefaultNameSystem() string {
	return "public"
}

func Packages(context *generator.Context, arguments *args.GeneratorArgs) generator.Packages {
	boilerplate, err := arguments.LoadGoBoilerplate()
	if err != nil {
		klog.Fatalf("Failed loading boilerplate: %v", err)
	}

	inputs := sets.NewString(context.Inputs...)
	packages := generator.Packages{}
	header := append([]byte(fmt.Sprintf("//go:build !%s\n// +build !%s\n\n", arguments.GeneratedBuildTag, arguments.GeneratedBuildTag)), boilerplate...)

	for i := range inputs {
		klog.V(5).Infof("Considering pkg %q", i)

		pkg := context.Universe[i]
		if pkg == nil {
			// If the input had no Go files, for example.
			continue
		}

		klog.V(3).Infof("Package %q needs generation", i)
		path := pkg.Path
		// if the source path is within a /vendor/ directory (for example,
		// k8s.io/kubernetes/vendor/k8s.io/apimachinery/pkg/apis/meta/v1), allow
		// generation to output to the proper relative path (under vendor).
		// Otherwise, the generator will create the file in the wrong location
		// in the output directory.
		// TODO: build a more fundamental concept in gengo for dealing with modifications
		// to vendored packages.
		if strings.HasPrefix(pkg.SourcePath, arguments.OutputBase) {
			expandedPath := strings.TrimPrefix(pkg.SourcePath, arguments.OutputBase)
			if strings.Contains(expandedPath, "/vendor/") {
				path = expandedPath
			}
		}
		packages = append(packages,
			&generator.DefaultPackage{
				PackageName: strings.Split(filepath.Base(pkg.Path), ".")[0],
				PackagePath: path,
				HeaderText:  header,
				GeneratorFunc: func(c *generator.Context) (generators []generator.Generator) {
					return []generator.Generator{
						NewGenDeepCopy(arguments.OutputFileBaseName, pkg.Path),
					}
				},
				FilterFunc: func(c *generator.Context, t *types.Type) bool {
					return t.Name.Package == pkg.Path
				},
			})
	}

	return packages
}

// genDeepCopy produces a file with autogenerated deep-copy functions.
type genDeepCopy struct {
	generator.DefaultGen
	targetPackage string
	imports       namer.ImportTracker
}

func NewGenDeepCopy(sanitizedName, targetPackage string) generator.Generator {
	return &genDeepCopy{
		DefaultGen: generator.DefaultGen{
			OptionalName: sanitizedName,
		},
		targetPackage: targetPackage,
		imports:       generator.NewImportTracker(),
	}
}

func (g *genDeepCopy) Namers(c *generator.Context) namer.NameSystems {
	// Have the raw namer for this file track what it imports.
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.targetPackage, g.imports),
	}
}

func (g *genDeepCopy) Filter(c *generator.Context, t *types.Type) bool {
	if !copyableType(t) {
		klog.V(2).Infof("Type %v is not copyable", t)
		return false
	}
	klog.V(4).Infof("Type %v is copyable", t)
	return true
}

func copyableType(t *types.Type) bool {
	// Filter out private types.
	if namer.IsPrivateGoName(t.Name.Name) {
		return false
	}

	if extractIgnoreTag(t) {
		return false
	}

	if t.Kind == types.Alias {
		return t.Underlying.Kind != types.Builtin || copyableType(t.Underlying)
	}

	if t.Kind != types.Struct {
		return false
	}

	return true
}

func underlyingType(t *types.Type) *types.Type {
	for t.Kind == types.Alias {
		t = t.Underlying
	}
	return t
}

func (g *genDeepCopy) isOtherPackage(pkg string) bool {
	if pkg == g.targetPackage {
		return false
	}
	if strings.HasSuffix(pkg, "\""+g.targetPackage+"\"") {
		return false
	}
	return true
}

func (g *genDeepCopy) Imports(c *generator.Context) (imports []string) {
	importLines := []string{}
	for _, singleImport := range g.imports.ImportLines() {
		if g.isOtherPackage(singleImport) {
			importLines = append(importLines, singleImport)
		}
	}
	return importLines
}

func (g *genDeepCopy) Init(c *generator.Context, w io.Writer) error {
	return nil
}

func (g *genDeepCopy) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	klog.V(5).Infof("Generating deepcopy function for type %v", t)

	sw := generator.NewSnippetWriter(w, c, "$", "$")
	sw.Do("// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.\n", generator.Args{})

	g.newBuilderFunc(sw, t)
	g.structBuilder(sw, t)
	g.structMethods(sw, t)
	g.structMethodBuild(sw, t)

	return sw.Error()
}

func (g *genDeepCopy) newBuilderFunc(sw *generator.SnippetWriter, t *types.Type) {
	args := generator.Args{
		"type": t,
		"name": t.Name.Name,
	}
	sw.Do("func New$.name$Builder() *$.type|raw$Builder {\n", args)
	sw.Do("builder := &$.type|raw$Builder{}\n", args)
	sw.Do("builder.model = $.type|raw${}\n", args)

	callMethods := extractNewMethodCallTag(t)
	for _, method := range callMethods {
		sw.Do("builder.model.$.method$()\n", generator.Args{"method": method})
	}

	for _, m := range t.Members {
		mt := m.Type
		umt := underlyingType(mt)
		// pumt := umt
		if umt.Kind == types.Pointer {
			umt = umt.Elem
		}

		argsMember := generator.Args{
			"name":       types.ParseFullyQualifiedName(umt.Name.Name).Name,
			"nameMethod": strings.ToLower(m.Name),
		}
		if umt.Kind == types.Slice {
			if !umt.Elem.IsPrimitive() {
				sw.Do("builder.$.nameMethod$ = []*$.name$Builder{}\n", argsMember)
			}
		} else if umt.Kind == types.Map {
			if !g.isOtherPackage(umt.Name.Package) || !g.isOtherPackage(types.ParseFullyQualifiedName(umt.Name.Name).Package) {
				if !umt.Elem.IsPrimitive() {
					argsMember["mapKey"] = umt.Key.Name.Name
					sw.Do("builder.$.nameMethod$ = map[$.mapKey$]*$.name$Builder{}\n", argsMember)
				}
			}
		} else if umt.Kind == types.Struct && mt.Kind != types.Pointer {
			if m.Embedded {
				sw.Do("builder.$.name$Builder = *New$.name$Builder()\n", argsMember)
			} else if !g.isOtherPackage(umt.Name.Package) || !g.isOtherPackage(types.ParseFullyQualifiedName(umt.Name.Name).Package) {
				sw.Do("builder.$.nameMethod$ = New$.name$Builder()\n", argsMember)
			}
		}
	}
	sw.Do("return builder\n", generator.Args{})
	sw.Do("}\n\n", generator.Args{})
}

func (g *genDeepCopy) structBuilder(sw *generator.SnippetWriter, t *types.Type) {
	args := generator.Args{
		"type": t,
	}
	sw.Do("type $.type|raw$Builder struct {\n", args)
	sw.Do("model $.type|raw$\n", args)
	for _, m := range t.Members {
		mt := m.Type
		umt := underlyingType(mt)
		// pumt := umt
		if umt.Kind == types.Pointer {
			umt = mt.Elem
		}

		argsMember := generator.Args{
			"name":     types.ParseFullyQualifiedName(umt.Name.Name).Name,
			"property": strings.ToLower(m.Name),
		}
		if umt.Kind == types.Slice {
			if !umt.Elem.IsPrimitive() {
				sw.Do("$.property$ []*$.name$Builder \n", argsMember)
			}
		} else if umt.Kind == types.Map {
			if !g.isOtherPackage(umt.Name.Package) || !g.isOtherPackage(types.ParseFullyQualifiedName(umt.Name.Name).Package) {
				if !umt.Elem.IsPrimitive() {
					argsMember["mapKey"] = umt.Key.Name.Name
					sw.Do("$.property$ map[$.mapKey$]*$.name$Builder \n", argsMember)
				}
			}
		} else if umt.Kind == types.Struct {
			if m.Embedded {
				pointer := ""
				if mt.Kind == types.Pointer {
					pointer = "*"
				}
				sw.Do(fmt.Sprintf("%s$.name$Builder\n", pointer), argsMember)

			} else if !g.isOtherPackage(umt.Name.Package) || !g.isOtherPackage(types.ParseFullyQualifiedName(umt.Name.Name).Package) {
				sw.Do("$.property$ *$.name$Builder\n", argsMember)
			}

		}
	}
	sw.Do("}\n", generator.Args{})
}

func (g *genDeepCopy) structMethods(sw *generator.SnippetWriter, t *types.Type) {
	for _, m := range t.Members {
		mt := m.Type
		umt := underlyingType(mt)
		// pumt := umt
		if umt.Kind == types.Pointer {
			umt = umt.Elem
		}

		argsMember := generator.Args{
			"typeBase":   t,
			"type":       umt,
			"typeAlias":  mt,
			"name":       m.Name,
			"nameMethod": strings.ToLower(m.Name),
		}

		if umt.Kind == types.Unsupported {
			klog.V(5).Infof("type unsupported %v %v", t, m.Name)
		} else if umt.IsPrimitive() {
			sw.Do("func (b *$.typeBase|raw$Builder) $.name$(input $.typeAlias|raw$)  {\n", argsMember)
			sw.Do("b.model.$.name$ = input\n", argsMember)
			sw.Do("}\n\n", generator.Args{})
		} else if umt.Kind == types.Slice {
			if umt.Elem.IsPrimitive() {
				sw.Do("func (b *$.typeBase|raw$Builder) $.name$(input $.typeAlias|raw$)  {\n", argsMember)
				sw.Do("b.model.$.name$ = input\n", argsMember)
				sw.Do("}\n\n", generator.Args{})
			} else {
				argsMember["nameNew"] = types.ParseFullyQualifiedName(umt.Elem.Name.Name).Name
				sw.Do("func (b *$.typeBase|raw$Builder) Add$.name$() *$.nameNew$Builder {\n", argsMember)
				sw.Do("builder := New$.nameNew$Builder()\n", argsMember)
				sw.Do("b.$.nameMethod$ = append(b.$.nameMethod$, builder)\n", argsMember)
				sw.Do("return builder\n", argsMember)
				sw.Do("}\n\n", generator.Args{})
			}
		} else if umt.Kind == types.Map {
			if umt.Elem.IsPrimitive() || g.isOtherPackage(umt.Name.Package) || g.isOtherPackage(types.ParseFullyQualifiedName(umt.Name.Name).Package) {
				sw.Do("func (b *$.typeBase|raw$Builder) $.name$(input $.typeAlias|raw$)  {\n", argsMember)
				sw.Do("b.model.$.name$ = input\n", argsMember)
				sw.Do("}\n\n", generator.Args{})
			} else {
				argsMember["mapKey"] = umt.Key.Name.Name
				argsMember["nameNew"] = types.ParseFullyQualifiedName(umt.Elem.Name.Name).Name
				sw.Do("func (b *$.typeBase|raw$Builder) Add$.name$(key $.mapKey$) *$.nameNew$Builder {\n", argsMember)
				sw.Do("builder := New$.nameNew$Builder()\n", argsMember)
				sw.Do("b.$.nameMethod$[key] = builder\n", argsMember)
				sw.Do("return builder\n", argsMember)
				sw.Do("}\n\n", generator.Args{})
			}
		} else if umt.Kind == types.Struct {
			if m.Embedded {
				ignoreMethods := extractEmbbedIgnoreMethodTag(t)
				ignore := false
				for _, method := range ignoreMethods {
					if method == argsMember["name"] {
						ignore = true
					}
				}

				if !ignore {
					sw.Do("func (b *$.typeBase|raw$Builder) $.name$() *$.type|raw$Builder {\n", argsMember)
					if mt.Kind == types.Pointer {
						sw.Do("if b.$.name$Builder == nil {\n", argsMember)
						sw.Do("b.$.name$Builder = New$.type|raw$Builder()\n", argsMember)
						sw.Do("}\n", generator.Args{})
						sw.Do("return b.$.name$Builder\n", argsMember)
					} else {
						sw.Do("return &b.$.name$Builder\n", argsMember)
					}
					sw.Do("}\n\n", generator.Args{})
				}
			} else if !g.isOtherPackage(umt.Name.Package) || !g.isOtherPackage(types.ParseFullyQualifiedName(umt.Name.Name).Package) {
				sw.Do("func (b *$.typeBase|raw$Builder) $.name$() *$.type|raw$Builder {\n", argsMember)
				if mt.Kind == types.Pointer {
					sw.Do("if b.$.nameMethod$ == nil {\n", argsMember)
					sw.Do("b.$.nameMethod$ = New$.type|raw$Builder()\n", argsMember)
					sw.Do("}\n", generator.Args{})
				}
				sw.Do("return b.$.nameMethod$\n", argsMember)
				sw.Do("}\n\n", generator.Args{})
			} else {
				sw.Do("func (b *$.typeBase|raw$Builder) $.name$(input $.typeAlias|raw$)  {\n", argsMember)
				sw.Do("b.model.$.name$ = input\n", argsMember)
				sw.Do("}\n\n", generator.Args{})
			}
		}
	}
}

func (g *genDeepCopy) structMethodBuild(sw *generator.SnippetWriter, t *types.Type) {
	args := generator.Args{
		"type": t,
	}

	sw.Do("func (b *$.type|raw$Builder) Build() $.type|raw$ {\n", args)
	for _, m := range t.Members {
		mt := m.Type
		umt := underlyingType(mt)
		// pumt := umt
		if umt.Kind == types.Pointer {
			umt = umt.Elem
		}

		argsMember := generator.Args{
			"name":       m.Name,
			"nameMethod": strings.ToLower(m.Name),
		}
		if umt.Kind == types.Unsupported {
			klog.V(5).Infof("type unsupported %v %v", t, m.Name)
		} else if umt.Kind == types.Slice {
			if !umt.Elem.IsPrimitive() {
				argsSlice := generator.Args{"name": m.Name, "type": umt.Elem}
				sw.Do("b.model.$.name$ = []$.type|raw${}\n", argsSlice)
				sw.Do("for _, v := range b.$.nameMethod$ {\n", argsMember)
				if umt.Elem.Kind == types.Pointer {
					sw.Do("vv := v.Build()\n", generator.Args{})
					sw.Do("b.model.$.name$ = append(b.model.$.name$, &vv)\n", argsSlice)
				} else {
					sw.Do("b.model.$.name$ = append(b.model.$.name$, v.Build())\n", argsSlice)
				}
				sw.Do("}\n", generator.Args{})
			}
		} else if umt.Kind == types.Map {
		} else if umt.Kind == types.Struct {
			if m.Embedded {
				if mt.Kind == types.Pointer {
					sw.Do("if b.$.name$Builder != nil {\n", argsMember)
					sw.Do("$.nameMethod$ := b.$.name$Builder.Build() \n", argsMember)
					sw.Do("b.model.$.name$ = &$.nameMethod$ \n", argsMember)
					sw.Do("}\n", generator.Args{})
				} else {
					sw.Do("b.model.$.name$ = b.$.name$Builder.Build() \n", argsMember)
				}
			} else if !g.isOtherPackage(umt.Name.Package) || !g.isOtherPackage(types.ParseFullyQualifiedName(umt.Name.Name).Package) {
				if mt.Kind == types.Pointer {
					sw.Do("if b.$.nameMethod$ != nil {\n", argsMember)
					sw.Do("$.nameMethod$ := b.$.nameMethod$.Build() \n", argsMember)
					sw.Do("b.model.$.name$ = &$.nameMethod$\n", argsMember)
					sw.Do("}\n", generator.Args{})
				} else {
					sw.Do("b.model.$.name$ = b.$.nameMethod$.Build()\n", argsMember)
				}
			}
		}
	}
	sw.Do("return b.model\n", generator.Args{})
	sw.Do("}\n\n", generator.Args{})
}
