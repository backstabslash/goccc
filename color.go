package main

import (
	"fmt"
	"os"
)

var noColorFlag bool

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiFaint  = "\033[2m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

func init() {
	if os.Getenv("NO_COLOR") != "" {
		noColorFlag = true
	}
}

type style struct {
	code string
}

var (
	bold = &style{ansiBold}
	cyan = &style{ansiCyan}
	dim  = &style{ansiFaint}
)

func (s *style) wrap(str string) string {
	if noColorFlag {
		return str
	}
	return s.code + str + ansiReset
}

func (s *style) Sprintf(format string, a ...interface{}) string {
	return s.wrap(fmt.Sprintf(format, a...))
}

func (s *style) Printf(format string, a ...interface{}) {
	fmt.Print(s.Sprintf(format, a...))
}

func (s *style) Println(a ...interface{}) {
	if noColorFlag {
		fmt.Println(a...)
		return
	}
	fmt.Print(s.code)
	fmt.Print(a...)
	fmt.Println(ansiReset)
}

func redString(s string) string {
	if noColorFlag {
		return s
	}
	return ansiRed + s + ansiReset
}

func yellowString(s string) string {
	if noColorFlag {
		return s
	}
	return ansiYellow + s + ansiReset
}
