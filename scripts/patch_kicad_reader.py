#!/usr/bin/env python3
"""Patch complémentaire du lecteur KiCad : pad/segment/via via resolveNetRef
+ définition des méthodes resolveNetRef/registerNet (la première passe
MultiEdit n'avait appliqué que struct/init/déclarations)."""
from pathlib import Path

F = Path("/home/z/my-project/yahriacad/backend/internal/infrastructure/fileio/reader/kicad.go")
src = F.read_text(encoding="utf-8")

reps = []

# 1) Bloc net des pastilles -> resolveNetRef
old_pad = '''                // (net N "NAME") : association électrique de la pastille.
                if netNode := padNode.child("net"); netNode != nil {
                        args := netNode.args()
                        if len(args) > 0 {
                                num := sexprInt(args[0])
                                name := ""
                                if len(args) > 1 {
                                        name = args[1]
                                }
                                if num > 0 && (name != "" || st.netNames[num] != "") {
                                        if name != "" {
                                                if _, seen := st.netNames[num]; !seen {
                                                        st.netOrder = append(st.netOrder, num)
                                                }
                                                st.netNames[num] = name
                                        }
                                        pad.Net = st.netName(num)
                                        st.netUsed[num] = true
                                        st.conns[num] = append(st.conns[num],
                                                schematicPinRef{ref: kf.ref, pin: padName})
                                }
                        }
                }'''
new_pad = '''                // (net N "NAME") — KiCad <= 9 — ou (net "NAME") — KiCad 10 :
                // association électrique de la pastille.
                if netNode := padNode.child("net"); netNode != nil {
                        if num, name, ok := st.resolveNetRef(netNode); ok && num > 0 &&
                                (name != "" || st.netNames[num] != "") {
                                st.registerNet(num, name)
                                pad.Net = st.netName(num)
                                st.netUsed[num] = true
                                st.conns[num] = append(st.conns[num],
                                        schematicPinRef{ref: kf.ref, pin: padName})
                        }
                }'''
reps.append(("pad", old_pad, new_pad))

# 2) Segment
old_seg = '''        if net := seg.child("net"); net != nil {
                num := sexprInt(net.arg(0))
                if num > 0 {
                        track.Net = st.netName(num)
                        st.netUsed[num] = true
                }
        }'''
new_seg = '''        if net := seg.child("net"); net != nil {
                if num, _, ok := st.resolveNetRef(net); ok && num > 0 {
                        track.Net = st.netName(num)
                        st.netUsed[num] = true
                }
        }'''
reps.append(("segment", old_seg, new_seg))

# 3) Via
old_via = '''        if net := via.child("net"); net != nil {
                num := sexprInt(net.arg(0))
                if num > 0 {
                        v.Net = st.netName(num)
                        st.netUsed[num] = true
                }
        }'''
new_via = '''        if net := via.child("net"); net != nil {
                if num, _, ok := st.resolveNetRef(net); ok && num > 0 {
                        v.Net = st.netName(num)
                        st.netUsed[num] = true
                }
        }'''
reps.append(("via", old_via, new_via))

# 4) Méthodes resolveNetRef + registerNet après netName
old_netname = '''// netName resolves the name of a net number, with a deterministic fallback.
func (st *kicadParseState) netName(num int) string {
        if name := st.netNames[num]; name != "" {
                return name
        }
        return fmt.Sprintf("NET_%d", num)
}'''
new_netname = old_netname + '''

// resolveNetRef interprets a (net ...) node in the two supported formats :
//   - KiCad <= 9 : (net N "NAME") — numéro explicite, nom optionnel ;
//   - KiCad >= 10 (version 20260206) : (net "NAME") — nom sans numéro.
//
// Pour le format par nom seul, un numéro synthétique stable (>=
// netSyntheticBase) est attribué à la première rencontre, si bien que la
// suite du lecteur (netOrder, netUsed, conns) continue de travailler sur
// des entiers. ok=false pour un nœud vide ou un nom vide.
func (st *kicadParseState) resolveNetRef(node *sNode) (int, string, bool) {
        args := node.args()
        if len(args) == 0 {
                return 0, "", false
        }
        if num, err := strconv.Atoi(args[0]); err == nil {
                name := ""
                if len(args) > 1 {
                        name = args[1]
                }
                return num, name, true
        }
        name := args[0]
        if name == "" {
                return 0, "", false
        }
        num, seen := st.netIDs[name]
        if !seen {
                st.nextNetID++
                num = st.nextNetID
                st.netIDs[name] = num
        }
        return num, name, true
}

// registerNet enregistre une paire (numéro, nom) : netOrder à la première
// rencontre, netNames sans effacer un nom déjà connu.
func (st *kicadParseState) registerNet(num int, name string) {
        if num <= 0 {
                return
        }
        if _, seen := st.netNames[num]; !seen {
                st.netOrder = append(st.netOrder, num)
        }
        if name != "" {
                st.netNames[num] = name
        }
}'''
reps.append(("methodes", old_netname, new_netname))

for label, old, new in reps:
    if old not in src:
        raise SystemExit(f"ANCRE INTROUVABLE : {label}")
    if src.count(old) != 1:
        raise SystemExit(f"ANCRE NON UNIQUE ({src.count(old)}) : {label}")
    src = src.replace(old, new)
    print(f"OK {label}")

F.write_text(src, encoding="utf-8")
print("patch appliqué :", F)
