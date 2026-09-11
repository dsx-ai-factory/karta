// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package karta

import (
	"fmt"
	"slices"
	"strings"

	"github.com/itchyny/gojq"
)

// pathSegment is one hop of a writable path: a named field, a literal-string key,
// or an array iteration.
type pathSegment struct {
	field string
	key   string
	iter  bool
}

// parseWritablePath accepts exactly the statically assignable subset of jq: one
// term, root Identity or a field/literal-string Index, suffixes that are only
// field/literal-string indexes or [], Optional only as its own suffix after an
// allowed one. Everything else - pipes, functions, operators, constructions,
// literals, computed or numeric indexes, slices - is rejected.
func parseWritablePath(expr string) ([]pathSegment, bool) {
	trimmed := strings.TrimSpace(expr)
	parsed, err := gojq.Parse(trimmed)
	if err != nil || parsed == nil {
		return nil, false
	}
	if parsed.Left != nil || parsed.Right != nil || parsed.Op != 0 ||
		len(parsed.FuncDefs) > 0 || parsed.Imports != nil || parsed.Meta != nil {
		return nil, false
	}
	term := parsed.Term
	if term == nil {
		return nil, false
	}
	var segments []pathSegment
	switch term.Type {
	case gojq.TermTypeIdentity:
	case gojq.TermTypeIndex:
		segment, ok := indexSegment(term.Index)
		if !ok {
			return nil, false
		}
		segments = append(segments, segment)
	default:
		return nil, false
	}
	for _, suffix := range term.SuffixList {
		switch {
		case suffix.Index != nil:
			segment, ok := indexSegment(suffix.Index)
			if !ok {
				return nil, false
			}
			segments = append(segments, segment)
		case suffix.Iter:
			segments = append(segments, pathSegment{iter: true})
		case suffix.Optional:
			if len(segments) == 0 {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return segments, true
}

func indexSegment(index *gojq.Index) (pathSegment, bool) {
	if index == nil || index.End != nil || index.IsSlice {
		return pathSegment{}, false
	}
	if index.Name != "" {
		return pathSegment{field: index.Name}, true
	}
	if index.Str != nil && index.Str.Queries == nil {
		return pathSegment{key: index.Str.Str}, true
	}
	if index.Start != nil {
		if key, ok := constantString(index.Start); ok {
			return pathSegment{key: key}, true
		}
	}
	return pathSegment{}, false
}

func constantString(query *gojq.Query) (string, bool) {
	if query == nil || query.Left != nil || query.Right != nil || query.Op != 0 || query.Term == nil {
		return "", false
	}
	term := query.Term
	if term.Type != gojq.TermTypeString || term.Str == nil ||
		term.Str.Queries != nil || len(term.SuffixList) != 0 {
		return "", false
	}
	return term.Str.Str, true
}

// isWritablePath reports whether expr is a statically assignable path.
func isWritablePath(expr string) bool {
	_, ok := parseWritablePath(expr)
	return ok
}

// renderPath renders segments back to canonical jq.
func renderPath(segments []pathSegment) string {
	if len(segments) == 0 {
		return "."
	}
	var builder strings.Builder
	for _, segment := range segments {
		switch {
		case segment.iter:
			builder.WriteString("[]")
		case segment.key != "":
			fmt.Fprintf(&builder, "[%q]", segment.key)
		default:
			builder.WriteString("." + segment.field)
		}
	}
	rendered := builder.String()
	if strings.HasPrefix(rendered, "[") {
		rendered = "." + rendered
	}
	return rendered
}

// joinPath composes a base writable path with relative segments structurally,
// so a base of "." (core Pod) composes correctly.
func joinPath(base []pathSegment, relative ...pathSegment) string {
	combined := make([]pathSegment, 0, len(base)+len(relative))
	combined = append(combined, base...)
	combined = append(combined, relative...)
	return renderPath(combined)
}

// iterCount returns how many [] iterations the path contains.
func iterCount(segments []pathSegment) int {
	count := 0
	for _, segment := range segments {
		if segment.iter {
			count++
		}
	}
	return count
}

// sharesIterationBase reports whether two writable paths satisfy the v1
// WithInstances rule: each has exactly one [], identical segments through the
// iteration.
func sharesIterationBase(a, b []pathSegment) bool {
	if iterCount(a) != 1 || iterCount(b) != 1 {
		return false
	}
	prefix := func(segments []pathSegment) []pathSegment {
		for i, segment := range segments {
			if segment.iter {
				return segments[:i+1]
			}
		}
		return nil
	}
	return slices.Equal(prefix(a), prefix(b))
}
