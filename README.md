bolter
======

tl;dr:
-----

```
cargo build --release
sudo ln -s $PWD/target/release/bolter /usr/bin/ld.bolter
musl-clang -fuse-ld=bolter any.c
```

may or may not need some or all of -static-libc or editing ld.musl-clang to only link static libraries.
bolter does not support dynamic linking.


for science!
-------------

this is a research project aimed to improve content addressable storage  of statically linked position
independant executables.

We'd like to be able to store multiple statically linked binaries in a space efficient way without
knowing all the versions upfront, so neither regular compression nor the classic multicall binary will work.

"testbin" contains a hyper client/server with separate binaries for client and server, as well as a multicall.
They share large parts of library code. Some won't be shared because it's not used.
For example the linker strips hyper server stuff when linking the client.


|          | client  | server  | total | multicall   |
|----------|---------|---------|-------|-------------|
| debug    | 21M     | 19M     | 40M   | 22M  (55%)  |
| release  | 4.8M    | 4.7M    | 9.5M  | 5M   (52%)  |
| stripped | 1.4M    | 1.3M    | 2.7M  | 1.6M (59%)  |

multicall is obviously very efficient. we'll have to be pretty clever to get even close to that.
The stripped binary is what will end up on a target, so 59% is our benchmark.

testing dedup algos:

| alogrithm | compression | shards |
|-----------|-------------|--------|
|bup(15)    | 94.89%      | 595    |
|bup(13)    | 90.18%      | 2034   |
|bup(11)    | 85.13%      | 7320   |
|bup(7)     | 77.37%      | 104105 |
|bolter     | 62.42%      | 79     |


rolling hashes such as bup are inefficient because all library code is relocated.
Even if both binaries use the same library, the code will not match because the addresses are different.

the figure for bolter linked executable comes from combination with https://github.com/korhalio/archon
which recognizes binaries from the bolter linker (this project) and de-deuplicates exactly along the
object boundaries emitted by bolter.

