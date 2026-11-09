#!/usr/bin/env python3
# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
"""slim_glb.py IN OUT: keeps only goarmin_part in each node's extras, names nodes by their part,
drops the generator's unused data. The buffers are left as they are."""
import json
import struct
import sys


def read(path):
    b = open(path, "rb").read()
    magic, version, length = struct.unpack("<III", b[:12])
    assert magic == 0x46546C67 and version == 2
    off, chunks = 12, []
    while off < length:
        n, kind = struct.unpack("<II", b[off:off + 8])
        chunks.append((kind, b[off + 8:off + 8 + n]))
        off += 8 + n
    return chunks


def write(path, j, binary):
    js = json.dumps(j, separators=(",", ":")).encode()
    js += b" " * (-len(js) % 4)
    binary += b"\0" * (-len(binary) % 4)
    total = 12 + 8 + len(js) + 8 + len(binary)
    with open(path, "wb") as f:
        f.write(struct.pack("<III", 0x46546C67, 2, total))
        f.write(struct.pack("<II", len(js), 0x4E4F534A) + js)
        f.write(struct.pack("<II", len(binary), 0x004E4942) + binary)


def main(src, dst):
    chunks = read(src)
    j = json.loads(chunks[0][1])
    binary = chunks[1][1]
    for nd in j["nodes"]:
        part = nd.get("extras", {}).get("goarmin_part")
        nd.pop("extras", None)
        if part:
            nd["name"] = part
            nd["extras"] = {"goarmin_part": part}
    for m in j.get("meshes", []):
        m.pop("extras", None)
    for s in j.get("scenes", []):
        s.pop("extras", None)
    j.get("asset", {}).pop("extras", None)
    write(dst, j, binary)
    print(dst, len(open(dst, "rb").read()))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
