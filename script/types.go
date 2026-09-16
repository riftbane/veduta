package script

import _ "embed"

// Types is the Lua API as a Lua Language Server definition file (---@meta): what editors
// complete and check a script game against. veduta init and upgrade write it into a project
// as .veduta/lua/veduta.d.lua.
//
//go:embed veduta.d.lua
var Types []byte
