module github.com/Nekhaevalex/fws

go 1.20

require (
	github.com/Nekhaevalex/fwsprotocol v0.0.4
	github.com/Nekhaevalex/queue v0.0.0-20230903204443-49f416cff44e
	github.com/nsf/termbox-go v1.1.1
)

replace github.com/Nekhaevalex/queue v0.0.0-20230903204443-49f416cff44e => ../../queue

require (
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/rivo/uniseg v0.4.4 // indirect
)
