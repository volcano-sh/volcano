package main // note!!! package must be named main

import (
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const PluginName = "magic"

type magicPlugin struct{}

func (mp *magicPlugin) Name() string {
	return PluginName
}

func New(arguments framework.Arguments) framework.Plugin { // `New` is PluginBuilder
	return &magicPlugin{}
}

func (mp *magicPlugin) OnSessionOpen(ssn *framework.Session) {}

func (mp *magicPlugin) OnSessionClose(ssn *framework.Session) {}
