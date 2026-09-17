package sigil

import "github.com/teranos/QNTX/plugin/grpc/protocol"

// The Go types are what the node works with, and proto is their shape at a
// boundary (ADR-006): the browser naming a sigil in a reach line, a plugin
// saying what it can do. Everything crosses but Answer, which is a function.

// Proto is the signum as it crosses.
func (s Signum) Proto() *protocol.Signum {
	crossed := &protocol.Signum{Name: s.Name}
	for _, sigil := range s.Sigils {
		crossed.Sigils = append(crossed.Sigils, sigil.Proto())
	}
	return crossed
}

// Proto is the sigil as it crosses.
func (s Sigil) Proto() *protocol.Sigil {
	crossed := &protocol.Sigil{
		Name: s.Name,
		Does: s.Does,
		Http: &protocol.Endpoint{Method: s.HTTP.Method, Path: s.HTTP.Path},
	}
	for _, param := range s.Takes {
		crossed.Takes = append(crossed.Takes, &protocol.Param{
			Name: param.Name, Says: param.Says, Required: param.Required, OneOf: param.OneOf,
		})
	}
	for _, field := range s.Gives {
		crossed.Gives = append(crossed.Gives, &protocol.Field{Name: field.Name, Says: field.Says})
	}
	return crossed
}

// Proto is the refusal as it crosses, the same whichever binding carries it.
func (r Refusal) Proto() *protocol.Refusal {
	return &protocol.Refusal{Why: string(r.Why), Param: r.Param, Says: r.Says}
}
