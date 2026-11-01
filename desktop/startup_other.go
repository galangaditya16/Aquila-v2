//go:build !windows

package main

import "log"

func startupErrorHandler() func(error) { return nil }

func startupFatal(err error) { log.Fatal(err) }

func startupRecover() {}

func reportUnexpectedExit() {}
