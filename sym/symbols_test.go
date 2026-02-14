package sym

import (
	"testing"
	"unicode/utf8"
)

func TestSymbolToCommandAndCommandToSymbolAreBidirectional(t *testing.T) {
	for symbol, cmd := range SymbolToCommand {
		got, ok := CommandToSymbol[cmd]
		if !ok {
			t.Errorf("SymbolToCommand has %q → %q, but CommandToSymbol has no entry for %q", symbol, cmd, cmd)
			continue
		}
		if got != symbol {
			t.Errorf("bidirectional mismatch: SymbolToCommand[%q] = %q, but CommandToSymbol[%q] = %q", symbol, cmd, cmd, got)
		}
	}

	for cmd, symbol := range CommandToSymbol {
		got, ok := SymbolToCommand[symbol]
		if !ok {
			t.Errorf("CommandToSymbol has %q → %q, but SymbolToCommand has no entry for %q", cmd, symbol, symbol)
			continue
		}
		if got != cmd {
			t.Errorf("bidirectional mismatch: CommandToSymbol[%q] = %q, but SymbolToCommand[%q] = %q", cmd, symbol, symbol, got)
		}
	}
}

func TestMapsHaveSameSize(t *testing.T) {
	if len(SymbolToCommand) != len(CommandToSymbol) {
		t.Errorf("map size mismatch: SymbolToCommand has %d entries, CommandToSymbol has %d",
			len(SymbolToCommand), len(CommandToSymbol))
	}
}

func TestCommandDescriptionsCoversAllCommands(t *testing.T) {
	for cmd := range CommandToSymbol {
		if _, ok := CommandDescriptions[cmd]; !ok {
			t.Errorf("CommandDescriptions missing entry for command %q", cmd)
		}
	}
}

func TestCommandDescriptionsHasNoExtraEntries(t *testing.T) {
	for cmd := range CommandDescriptions {
		if _, ok := CommandToSymbol[cmd]; !ok {
			t.Errorf("CommandDescriptions has entry for %q which is not in CommandToSymbol", cmd)
		}
	}
}

func TestPaletteOrderContainsValidSymbols(t *testing.T) {
	for i, symbol := range PaletteOrder {
		if _, ok := SymbolToCommand[symbol]; !ok {
			t.Errorf("PaletteOrder[%d] = %q is not in SymbolToCommand", i, symbol)
		}
	}
}

func TestPaletteOrderHasNoDuplicates(t *testing.T) {
	seen := make(map[string]int, len(PaletteOrder))
	for i, symbol := range PaletteOrder {
		if prev, ok := seen[symbol]; ok {
			t.Errorf("PaletteOrder has duplicate %q at indices %d and %d", symbol, prev, i)
		}
		seen[symbol] = i
	}
}

func TestSymbolsAreValidUnicode(t *testing.T) {
	for symbol := range SymbolToCommand {
		if !utf8.ValidString(symbol) {
			t.Errorf("symbol %q is not valid UTF-8", symbol)
		}
		if utf8.RuneCountInString(symbol) == 0 {
			t.Errorf("symbol for command %q is empty", SymbolToCommand[symbol])
		}
	}
}

func TestNoDuplicateSymbolValues(t *testing.T) {
	seen := make(map[string]string, len(SymbolToCommand))
	for symbol, cmd := range SymbolToCommand {
		if prevCmd, ok := seen[symbol]; ok {
			t.Errorf("duplicate symbol %q: used by both %q and %q", symbol, prevCmd, cmd)
		}
		seen[symbol] = cmd
	}
}

func TestNoDuplicateCommandValues(t *testing.T) {
	seen := make(map[string]string, len(CommandToSymbol))
	for cmd, symbol := range CommandToSymbol {
		if prevSymbol, ok := seen[cmd]; ok {
			t.Errorf("duplicate command %q: maps to both %q and %q", cmd, prevSymbol, symbol)
		}
		seen[cmd] = symbol
	}
}

func TestCommandsAreInCommandToSymbol(t *testing.T) {
	for _, cmd := range Commands {
		if _, ok := CommandToSymbol[cmd]; !ok {
			t.Errorf("Commands contains %q which is not in CommandToSymbol", cmd)
		}
	}
}
