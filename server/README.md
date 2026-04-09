# Server

The QNTX server, yeah there is a lot in here.

websockets a lot.

I like what it ended up becoming for a lot of the parts of the system, it's really maturing.
I like that we treat state very seriously.
I like that i tried a lot of different ideas, not everything works out.
I'm happy about the Plugin pattern, where you can do a lot in a gRPC plugin, what we would otherwise be doing in this part of QNTX, ideally this package isn't as large as it is now.

This thing seems like a constant work in progress, but honestly this part of the system seems to have become super stable, im still not happy about howmany files there are here, but it's just cow things came to be. 

I would really like to see a cleaner code base, like moving somethings into their  own subpackages, but this is what you get when you vibecode things.

I don't know when the LLM is going to get a lot better at writing concurrent code.

SOmetimes it really does feel like the only way is by writing another integration test for the functionality you desire, even if its a very small thing, especially thn actually.

I took avery heavily tdd approach in many of the packages, so when refactoring i think its important to keep that in mind.

there is this thing called typegen that generates the api documentation, you can find it in docs/api/ 

- PROSE: The prose question, currently I am writinganother plugin called Voor, prose was meant to be a visual way in QNTX to craft the perfect prompt, and use different strats for finding out what works best. Voor seems to be taking over this role and is supposed to do it much better, Loom seems likely to be the UI for Voor looking forward. but that leaved the question of what we do with this functionality in QNTX, im starting to feel like it should just be taken out actually, deprecate Prose, also in the UI, it uses a prosemirror edit, and I can't get it to work for me in a way that its going to replace the way i promptright now, there is a lot about Prose and the built-in documentation, that just isnt pulling its weight.
- INTERNAL: this thing is basically QNTX, it belongs in internal/
