// Copyright (c) 2011 Florian Weimer. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
// * Redistributions of source code must retain the above copyright
//   notice, this list of conditions and the following disclaimer.
//
// * Redistributions in binary form must reproduce the above copyright
//   notice, this list of conditions and the following disclaimer in the
//   documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

// Package pcre provides access to the Perl Compatible Regular
// Expresion library, PCRE.
//
// It implements two main types, Regexp and Matcher.  Regexp objects
// store a compiled regular expression.  They are immutable.
// Compilation of regular expressions using Compile or MustCompile is
// slightly expensive, so these objects should be kept and reused,
// instead of compiling them from scratch for each matching attempt.
//
// Matcher objects keeps the results of a match against a []byte or
// string subject.  The Group and GroupString functions provide access
// to capture groups; both versions work no matter if the subject was a
// []byte or string, but the version with the matching type is slightly
// more efficient.
//
// Matcher objects contain some temporary space and refer the original
// subject.  They are mutable and can be reused (using Match,
// MatchString, Reset or ResetString).
//
// For details on the regular expression language implemented by this
// package and the flags defined below, see the PCRE documentation.
package pcre

// #cgo pkg-config: libpcre
// #include <pcre.h>
// #include <string.h>
import "C"

import (
	"fmt"
	"strconv"
	"unsafe"
)

// Flags for Compile and Match functions.
const (
	ANCHORED        = C.PCRE_ANCHORED
	BSR_ANYCRLF     = C.PCRE_BSR_ANYCRLF
	BSR_UNICODE     = C.PCRE_BSR_UNICODE
	NEWLINE_ANY     = C.PCRE_NEWLINE_ANY
	NEWLINE_ANYCRLF = C.PCRE_NEWLINE_ANYCRLF
	NEWLINE_CR      = C.PCRE_NEWLINE_CR
	NEWLINE_CRLF    = C.PCRE_NEWLINE_CRLF
	NEWLINE_LF      = C.PCRE_NEWLINE_LF
	NO_UTF8_CHECK   = C.PCRE_NO_UTF8_CHECK
)

// Flags for Compile functions
const (
	CASELESS          = C.PCRE_CASELESS
	DOLLAR_ENDONLY    = C.PCRE_DOLLAR_ENDONLY
	DOTALL            = C.PCRE_DOTALL
	DUPNAMES          = C.PCRE_DUPNAMES
	EXTENDED          = C.PCRE_EXTENDED
	EXTRA             = C.PCRE_EXTRA
	FIRSTLINE         = C.PCRE_FIRSTLINE
	JAVASCRIPT_COMPAT = C.PCRE_JAVASCRIPT_COMPAT
	MULTILINE         = C.PCRE_MULTILINE
	NO_AUTO_CAPTURE   = C.PCRE_NO_AUTO_CAPTURE
	UNGREEDY          = C.PCRE_UNGREEDY
	UTF8              = C.PCRE_UTF8
)

// Flags for Match functions
const (
	NOTBOL            = C.PCRE_NOTBOL
	NOTEOL            = C.PCRE_NOTEOL
	NOTEMPTY          = C.PCRE_NOTEMPTY
	NOTEMPTY_ATSTART  = C.PCRE_NOTEMPTY_ATSTART
	NO_START_OPTIMIZE = C.PCRE_NO_START_OPTIMIZE
	PARTIAL_HARD      = C.PCRE_PARTIAL_HARD
	PARTIAL_SOFT      = C.PCRE_PARTIAL_SOFT
)

// Regexp holds a reference to a compiled regular expression.
// Use Compile or MustCompile to create such objects.
type Regexp struct {
	ptr []byte
}

// Number of bytes in the compiled pattern
func pcreSize(ptr *C.pcre) (size C.size_t) {
	C.pcre_fullinfo(ptr, nil, C.PCRE_INFO_SIZE, unsafe.Pointer(&size))
	return
}

// Number of capture groups
func pcreGroups(ptr *C.pcre) (count C.int) {
	C.pcre_fullinfo(ptr, nil,
		C.PCRE_INFO_CAPTURECOUNT, unsafe.Pointer(&count))
	return
}

// Move pattern to the Go heap so that we do not have to use a
// finalizer.  PCRE patterns are fully relocatable. (We do not use
// custom character tables.)
func toHeap(ptr *C.pcre) (re Regexp) {
	defer C.free(unsafe.Pointer(ptr))
	size := pcreSize(ptr)
	re.ptr = make([]byte, size)
	C.memcpy(unsafe.Pointer(&re.ptr[0]), unsafe.Pointer(ptr), size)
	return
}

// Compile the pattern and return a compiled regexp.
// If compilation fails, the second return value holds a *CompileError.
func Compile(pattern string, flags int) (Regexp, error) {
	pattern1 := C.CString(pattern)
	defer C.free(unsafe.Pointer(pattern1))
	if clen := int(C.strlen(pattern1)); clen != len(pattern) {
		return Regexp{}, &CompileError{
			Pattern: pattern,
			Message: "NUL byte in pattern",
			Offset:  clen,
		}
	}
	var errptr *C.char
	var erroffset C.int
	ptr := C.pcre_compile(pattern1, C.int(flags), &errptr, &erroffset, nil)
	if ptr == nil {
		return Regexp{}, &CompileError{
			Pattern: pattern,
			Message: C.GoString(errptr),
			Offset:  int(erroffset),
		}
	}
	heap := toHeap(ptr)
	return heap, nil
}

// MustCompile compiles the pattern.  If compilation fails, panic.
func MustCompile(pattern string, flags int) (re Regexp) {
	re, err := Compile(pattern, flags)
	if err != nil {
		panic(err)
	}
	return
}

// Groups returns the number of capture groups in the compiled pattern.
func (re Regexp) Groups() int {
	if re.ptr == nil {
		panic("Regexp.Groups: uninitialized")
	}
	return int(pcreGroups((*C.pcre)(unsafe.Pointer(&re.ptr[0]))))
}

// Matcher objects provide a place for storing match results.
// They can be created by the Matcher and MatcherString functions,
// or they can be initialized with Reset or ResetString.
type Matcher struct {
	re       Regexp
	groups   int
	ovector  []C.int // scratch space for capture offsets
	matches  bool    // last match was successful
	subjects string  // one of these fields is set to record the subject,
	subjectb []byte  // so that Group/GroupString can return slices
}

// Matcher creates a new matcher object, with the byte slice as subject.
// It also starts a first match. Obtain the result with Matches().
func (re Regexp) Matcher(subject []byte, flags int) (m *Matcher) {
	m = new(Matcher)
	m.Reset(re, subject, flags)
	return
}

// MatcherString creates a new matcher, with the specified subject string.
// It also starts a first match. Obtain the result with Matches().
func (re Regexp) MatcherString(subject string, flags int) (m *Matcher) {
	m = new(Matcher)
	m.ResetString(re, subject, flags)
	return
}

// Reset switches the matcher object to the specified pattern and subject.
// It also starts a first match. Obtain the result with Matches().
func (m *Matcher) Reset(re Regexp, subject []byte, flags int) {
	if re.ptr == nil {
		panic("Regexp.Matcher: uninitialized")
	}
	m.init(re)
	m.Match(subject, flags)
}

// ResetString switches the matcher object to the given pattern and subject.
// It also starts a first match. Obtain the result with Matches().
func (m *Matcher) ResetString(re Regexp, subject string, flags int) {
	if re.ptr == nil {
		panic("Regexp.Matcher: uninitialized")
	}
	m.init(re)
	m.MatchString(subject, flags)
}

func (m *Matcher) init(re Regexp) {
	m.matches = false
	if m.re.ptr != nil && &m.re.ptr[0] == &re.ptr[0] {
		// Skip group count extraction if the matcher has
		// already been initialized with the same regular
		// expression.
		return
	}
	m.re = re
	m.groups = re.Groups()
	if ovectorlen := 3 * (1 + m.groups); len(m.ovector) < ovectorlen {
		m.ovector = make([]C.int, ovectorlen)
	}
}

var nullbyte = []byte{0}

// Match tries to match the speficied byte array slice to the current
// pattern.  Returns true if the match succeeds.
func (m *Matcher) Match(subject []byte, flags int) bool {
	if m.re.ptr == nil {
		panic("Matcher.Match: uninitialized")
	}
	length := len(subject)
	m.subjects = ""
	m.subjectb = subject
	if length == 0 {
		subject = nullbyte // make first character adressable
	}
	subjectptr := (*C.char)(unsafe.Pointer(&subject[0]))
	return m.match(subjectptr, length, flags)
}

// MatchString tries to match the speficied subject string to
// the current pattern. Returns true if the match succeeds.
func (m *Matcher) MatchString(subject string, flags int) bool {
	if m.re.ptr == nil {
		panic("Matcher.Match: uninitialized")
	}
	length := len(subject)
	m.subjects = subject
	m.subjectb = nil
	if length == 0 {
		subject = "\000" // make first character addressable
	}
	// The following is a non-portable kludge to avoid a copy
	subjectptr := *(**C.char)(unsafe.Pointer(&subject))
	return m.match(subjectptr, length, flags)
}

func (m *Matcher) match(subjectptr *C.char, length, flags int) bool {
	rc := C.pcre_exec((*C.pcre)(unsafe.Pointer(&m.re.ptr[0])), nil,
		subjectptr, C.int(length),
		0, C.int(flags), &m.ovector[0], C.int(len(m.ovector)))
	switch {
	case rc >= 0:
		m.matches = true
		return true
	case rc == C.PCRE_ERROR_NOMATCH:
		m.matches = false
		return false
	case rc == C.PCRE_ERROR_BADOPTION:
		panic("PCRE.Match: invalid option flag")
	}
	panic("unexepected return code from pcre_exec: " +
		strconv.Itoa(int(rc)))
}

// Matches returns true if a previous call to Matcher, MatcherString, Reset,
// ResetString, Match or MatchString succeeded.
func (m *Matcher) Matches() bool {
	return m.matches
}

// Groups returns the number of groups in the current pattern.
func (m *Matcher) Groups() int {
	return m.groups
}

// Present returns true if the numbered capture group is present in the last
// match (performed by Matcher, MatcherString, Reset, ResetString,
// Match, or MatchString).  Group numbers start at 1.  A capture group
// can be present and match the empty string.
func (m *Matcher) Present(group int) bool {
	return m.ovector[2*group] >= 0
}

// Group returns the numbered capture group of the last match (performed by
// Matcher, MatcherString, Reset, ResetString, Match, or MatchString).
// Group 0 is the part of the subject which matches the whole pattern;
// the first actual capture group is numbered 1.  Capture groups which
// are not present return a nil slice.
func (m *Matcher) Group(group int) []byte {
	start := m.ovector[2*group]
	end := m.ovector[2*group+1]
	if start >= 0 {
		if m.subjectb != nil {
			return m.subjectb[start:end]
		}
		return []byte(m.subjects[start:end])
	}
	return nil
}

// ExtractString returns a slice of strings for a single match.
// The first string contains the complete match.
// Subsequent strings in the slice contain the captured groups.
// If there was no match then nil is returned.
func (m *Matcher) ExtractString() []string {
	if !m.matches {
		return nil
	}
	extract := make([]string, m.groups+1)
	extract[0] = m.subjects
	for i := 1; i <= m.groups; i++ {
		x0 := m.ovector[2*i]
		x1 := m.ovector[2*i+1]
		extract[i] = m.subjects[x0:x1]
	}
	return extract
}

// GroupIndices returns the numbered capture group positions of the last
// match (performed by Matcher, MatcherString, Reset, ResetString, Match,
// or MatchString). Group 0 is the part of the subject which matches
// the whole pattern; the first actual capture group is numbered 1.
// Capture groups which are not present return a nil slice.
func (m *Matcher) GroupIndices(group int) []int {
	start := m.ovector[2*group]
	end := m.ovector[2*group+1]
	if start >= 0 {
		return []int{int(start), int(end)}
	}
	return nil
}

// GroupString returns the numbered capture group as a string.  Group 0
// is the part of the subject which matches the whole pattern; the first
// actual capture group is numbered 1.  Capture groups which are not
// present return an empty string.
func (m *Matcher) GroupString(group int) string {
	start := m.ovector[2*group]
	end := m.ovector[2*group+1]
	if start >= 0 {
		if m.subjectb != nil {
			return string(m.subjectb[start:end])
		}
		return m.subjects[start:end]
	}
	return ""
}

// Index returns the start and end of the first match, if a previous
// call to Matcher, MatcherString, Reset, ResetString, Match or
// MatchString succeeded. loc[0] is the start and loc[1] is the end.
func (m *Matcher) Index() []int {
	if !m.Matches() {
		return nil
	}

	return []int{int(m.ovector[0]), int(m.ovector[1])}
}

func (m *Matcher) name2index(name string) (int, error) {
	if m.re.ptr == nil {
		panic("Matcher.Named: uninitialized")
	}
	name1 := C.CString(name)
	defer C.free(unsafe.Pointer(name1))
	var group int
	group = int(C.pcre_get_stringnumber(
		(*C.pcre)(unsafe.Pointer(&m.re.ptr[0])), name1))
	if group < 0 {
		return group, fmt.Errorf("Matcher.Named: unknown name: " + name)
	}
	return group, nil
}

// Named returns the value of the named capture group.  This is a nil slice
// if the capture group is not present.  Panics if the name does not
// refer to a group.
func (m *Matcher) Named(group string) ([]byte, error) {
	groupNum, err := m.name2index(group)
	if err != nil {
		return []byte{}, err
	}
	return m.Group(groupNum), nil
}

// NamedString returns the value of the named capture group,
// or an empty string if the capture group is not present.
// Panics if the name does not refer to a group.
func (m *Matcher) NamedString(group string) (string, error) {
	groupNum, err := m.name2index(group)
	if err != nil {
		return "", err
	}
	return m.GroupString(groupNum), nil
}

// NamedPresent returns true if the named capture group is present.
// Panics if the name does not refer to a group.
func (m *Matcher) NamedPresent(group string) (bool, error) {
	groupNum, err := m.name2index(group)
	if err != nil {
		return false, err
	}
	return m.Present(groupNum), nil
}

// FindIndex returns the start and end of the first match,
// or nil if no match.  loc[0] is the start and loc[1] is the end.
func (re *Regexp) FindIndex(bytes []byte, flags int) []int {
	m := re.Matcher(bytes, flags)
	if m.Matches() {
		return []int{int(m.ovector[0]), int(m.ovector[1])}
	}
	return nil
}

// ReplaceAll returns a copy of a byte slice
// where all pattern matches are replaced by repl.
func (re Regexp) ReplaceAll(bytes, repl []byte, flags int) []byte {
	m := re.Matcher(bytes, flags)
	r := []byte{}
	for m.matches {
		r = append(append(r, bytes[:m.ovector[0]]...), repl...)
		bytes = bytes[m.ovector[1]:]
		m.Match(bytes, flags)
	}
	return append(r, bytes...)
}

// ReplaceAllString is equivalent to ReplaceAll with string return type.
func (re Regexp) ReplaceAllString(in, repl string, flags int) string {
	return string(re.ReplaceAll([]byte(in), []byte(repl), flags))
}

// CompileError holds details about a compilation error,
// as returned by the Compile function.  The offset is
// the byte position in the pattern string at which the
// error was detected.
type CompileError struct {
	Pattern string // The failed pattern
	Message string // The error message
	Offset  int    // Byte position of error
}

// Error converts a compile error to a string
func (e *CompileError) Error() string {
	return e.Pattern + " (" + strconv.Itoa(e.Offset) + "): " + e.Message
}
