# Server

The QNTX server, yeah there is a lot in here.

This thing seems like a constant work in progress, but honestly this part of the system seems to have become super stable, im still not happy about howmany files there are here, but it's just cow things came to be. 

I would really like to see a cleaner code base, like moving somethings into their  own subpackages, but this is what you get when you vibecode things.

I don't know when the LLM is going to get a lot better at writing concurrent code.

SOmetimes it really does feel like the only way is by writing another integration test for the functionality you desire, even if its a very small thing, especially thn actually.

I took avery heavily tdd approach in many of the packages, so when refactoring i think its important to keep that in mind.

there is this thing called typegen that generates the api documentation, you can find it in docs/api/ 
