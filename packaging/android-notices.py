#!/usr/bin/env python3
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# android-notices.py POMS: writes to stdout the notices for the Android app's
# Java and Kotlin libraries, after notices.sh android's for its Go core. POMS
# is the folder Gradle's noticesPoms task fills: libraries.txt, the release
# build's libraries, and each one's POM and its parents', as
# group:artifact:version.pom. A library's license is its POM's, or its
# nearest parent's that names one. Then each license's text, once. A library
# whose license is not found, or is not one whose text is here, stops it, as
# in notices.sh: notices that miss one are not notices.
import pathlib
import sys
import xml.etree.ElementTree as ET

ROOT = pathlib.Path(__file__).resolve().parent.parent

# The licenses whose text is in packaging/licenses, by how POMs name them.
TEXTS = {"Apache-2.0": "Apache-2.0.txt"}
NAMES = {
    "the apache software license, version 2.0": "Apache-2.0",
    "the apache license, version 2.0": "Apache-2.0",
    "apache license, version 2.0": "Apache-2.0",
    "apache license 2.0": "Apache-2.0",
    "apache-2.0": "Apache-2.0",
    "apache 2.0": "Apache-2.0",
}
URLS = {"apache.org/licenses/license-2.0": "Apache-2.0"}


def die(msg):
    print(f"android-notices.py: {msg}", file=sys.stderr)
    sys.exit(1)


def pom(poms, coords):
    path = poms / f"{coords}.pom"
    if not path.exists():
        die(f"no POM for {coords} in {poms}")
    root = ET.parse(path).getroot()
    for e in root.iter():  # the POM namespace, dropped
        e.tag = e.tag.split("}")[-1]
    return root


def spdx(name, url):
    if name.strip().lower() in NAMES:
        return NAMES[name.strip().lower()]
    for u, s in URLS.items():
        if u in url.lower():
            return s
    return None


def licenses(poms, coords):
    """The library's licenses, from its POM or the nearest parent's that names one."""
    library = coords
    while True:
        root = pom(poms, coords)
        found = [(l.findtext("name", ""), l.findtext("url", "")) for l in root.findall("licenses/license")]
        if found:
            out = set()
            for name, url in found:
                s = spdx(name, url)
                if s is None:
                    die(f"{library}: no text here for its license {name!r} ({url})")
                out.add(s)
            return sorted(out)
        parent = root.find("parent")
        if parent is None:
            die(f"{library}: its POM names no license")
        coords = ":".join(parent.findtext(k, "").strip() for k in ("groupId", "artifactId", "version"))


def main():
    poms = pathlib.Path(sys.argv[1])
    libraries = (poms / "libraries.txt").read_text(encoding="utf-8").split()
    if not libraries:
        die(f"no libraries in {poms}/libraries.txt")
    rule = "=" * 78
    used = set()
    print(f"\n{rule}\n== The Java and Kotlin libraries of the app's screens\n")
    print("Each under the license that follows its name; the licenses' texts follow.\n")
    for coords in libraries:
        names = licenses(poms, coords)
        used.update(names)
        group, artifact, version = coords.split(":")
        print(f"{group}:{artifact} {version}: {', '.join(names)}")
    for name in sorted(used):
        print(f"\n{rule}\n== {name}\n")
        print((ROOT / "packaging/licenses" / TEXTS[name]).read_text(encoding="utf-8"), end="")


main()
