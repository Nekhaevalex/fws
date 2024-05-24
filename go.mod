module github.com/Nekhaevalex/fws

go 1.20

require (
	github.com/Nekhaevalex/fwsprotocol v0.0.4
	github.com/Nekhaevalex/queue v1.0.0
	github.com/nsf/termbox-go v1.1.1
)

replace github.com/Nekhaevalex/fwsprotocol => ../fwsprotocol/

require (
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/rivo/uniseg v0.4.4 // indirect
)
