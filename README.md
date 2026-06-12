# File Synchronization Engine

this is a highly experimental replacement for syncthing and other similar file sync tools.  it's my own take on it.
Some of the functionality was inspired by resiliosync and syncthing. So credit goes to them for concepts I've borrowed.

use at your own risk.  this may or may not work, I make no promises as to how well it'll function as this is still very early prototype phase.

full disclosure,  ai was used to help build this project.  Much of the code is my own, but I'm terrible at some things, especially building of gui's.  And I hate doing grunt work like troubleshooting github actions.  So yes,  ai was employeed to aid in creating this project. But AID only.  And for the forseeable future while trying to get the project to a state where it compiles on github, the ai worker will be churning away at the code for a bit.

I have not yet done any level of security testing on this. It's not clean and neat yet, it's kinda ugly.  Remember when I said it was a prototype?  If you want to complain about the code, or dependencies that aren't being used, and so on, remember it's a prototype.  I haven't gotten to the cleanup and fix bugs/security problems stage yet.  So user beware.  it's ugly, and will probably be that way for a bit longer.

also one more fair warning, some of the encryption used is placeholders.  it's not secure yet.  this is by design as it's easier for me to work with it in this state.  Much more robust encryption is planned and will be implimented as soon as I have working builds compiling. Yes it's secure enough that it won't send things in plain text,  but it's not using computationally expensive calculations yet so it probably won't be hard to break the encryption.  Do not use this for transfering sensitive data over the internet for now.
