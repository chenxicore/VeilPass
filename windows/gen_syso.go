//go:build ignore

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf16"
)

const manifestXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="2.0.0.0" processorArchitecture="amd64" name="Local.IsolatedBrowser" type="win32"/>
  <description>隔离浏览器</description>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>
`

type rsrcItem struct {
	typeID uint32
	nameID uint32
	langID uint32
	data   []byte
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: gen_syso <icon_dir> <out.syso>\n")
		os.Exit(1)
	}
	iconDir := os.Args[1]
	outPath := os.Args[2]

	sizes := []struct {
		file string
		dim  int
	}{
		{"icon_16x16.png", 16},
		{"icon_32x32.png", 32},
		{"icon_32x32@2x.png", 64},
		{"icon_128x128.png", 128},
		{"icon_256x256.png", 256},
	}

	var items []rsrcItem
	var grp bytes.Buffer
	// GRPICONDIR header: reserved=0, type=1 (icon), count
	binary.Write(&grp, binary.LittleEndian, uint16(0))
	binary.Write(&grp, binary.LittleEndian, uint16(1))
	binary.Write(&grp, binary.LittleEndian, uint16(len(sizes)))

	for i, spec := range sizes {
		pngBytes, err := os.ReadFile(filepath.Join(iconDir, spec.file))
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", spec.file, err)
			os.Exit(1)
		}
		iconID := uint16(i + 1)
		items = append(items, rsrcItem{
			typeID: 3, // RT_ICON
			nameID: uint32(iconID),
			langID: 0x0409,
			data:   pngBytes,
		})
		dimByte := uint8(spec.dim)
		if spec.dim >= 256 {
			dimByte = 0
		}
		// GRPICONDIRENTRY (14 bytes)
		grp.WriteByte(dimByte)                                     // bWidth
		grp.WriteByte(dimByte)                                     // bHeight
		grp.WriteByte(0)                                           // bColorCount
		grp.WriteByte(0)                                           // bReserved
		binary.Write(&grp, binary.LittleEndian, uint16(1))         // wPlanes
		binary.Write(&grp, binary.LittleEndian, uint16(32))        // wBitCount
		binary.Write(&grp, binary.LittleEndian, uint32(len(pngBytes))) // dwBytesInRes
		binary.Write(&grp, binary.LittleEndian, iconID)            // nID
	}

	items = append(items, rsrcItem{
		typeID: 14, // RT_GROUP_ICON
		nameID: 1,
		langID: 0x0409,
		data:   grp.Bytes(),
	})

	items = append(items, rsrcItem{
		typeID: 24, // RT_MANIFEST
		nameID: 1,
		langID: 0x0409,
		data:   []byte(manifestXML),
	})

	// Also build Version Info or keep clean RT_ICON + RT_GROUP_ICON + RT_MANIFEST
	_ = utf16.Encode([]rune(""))

	syso, err := buildCOFFRsrc(items)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build coff: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, syso, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write syso: %v\n", err)
		os.Exit(1)
	}
}

type coffReloc struct {
	virtualAddr uint32
	symbolIndex uint32
	relocType   uint16
}

func buildCOFFRsrc(items []rsrcItem) ([]byte, error) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].typeID != items[j].typeID {
			return items[i].typeID < items[j].typeID
		}
		if items[i].nameID != items[j].nameID {
			return items[i].nameID < items[j].nameID
		}
		return items[i].langID < items[j].langID
	})

	// Group by typeID -> nameID -> langID
	type nameGroup struct {
		nameID uint32
		items  []rsrcItem
	}
	type typeGroup struct {
		typeID uint32
		names  []nameGroup
	}

	var types []typeGroup
	for _, item := range items {
		if len(types) == 0 || types[len(types)-1].typeID != item.typeID {
			types = append(types, typeGroup{typeID: item.typeID})
		}
		tg := &types[len(types)-1]
		if len(tg.names) == 0 || tg.names[len(tg.names)-1].nameID != item.nameID {
			tg.names = append(tg.names, nameGroup{nameID: item.nameID})
		}
		tg.names[len(tg.names)-1].items = append(tg.names[len(tg.names)-1].items, item)
	}

	// Calculate directory layout inside .rsrc$01:
	// Level 1: 16-byte header + 8 * len(types)
	// Level 2: for each type: 16-byte header + 8 * len(tg.names)
	// Level 3: for each name: 16-byte header + 8 * len(ng.items)
	// Data entries: 16 bytes per item
	dirSize := 16 + 8*len(types)
	for _, tg := range types {
		dirSize += 16 + 8*len(tg.names)
		for _, ng := range tg.names {
			dirSize += 16 + 8*len(ng.items)
		}
	}
	dataEntriesOffset := dirSize
	rsrc01Size := dataEntriesOffset + 16*len(items)

	// Build .rsrc$02 payload (4-byte aligned each)
	var rsrc02 bytes.Buffer
	dataOffsets := make([]uint32, len(items))
	for i, it := range items {
		for rsrc02.Len()%4 != 0 {
			rsrc02.WriteByte(0)
		}
		dataOffsets[i] = uint32(rsrc02.Len())
		rsrc02.Write(it.data)
	}
	for rsrc02.Len()%4 != 0 {
		rsrc02.WriteByte(0)
	}

	// Build .rsrc$01 bytes + relocations pointing to symbol 1 (.rsrc$02)
	rsrc01 := make([]byte, rsrc01Size)
	var relocs []coffReloc

	writeDirHeader := func(off int, idCount int) {
		binary.LittleEndian.PutUint16(rsrc01[off+14:], uint16(idCount))
	}
	writeDirEntry := func(off int, id uint32, targetOff uint32, isSubdir bool) {
		binary.LittleEndian.PutUint32(rsrc01[off:], id)
		val := targetOff
		if isSubdir {
			val |= 0x80000000
		}
		binary.LittleEndian.PutUint32(rsrc01[off+4:], val)
	}

	writeDirHeader(0, len(types))
	l2Off := 16 + 8*len(types)
	l3Start := l2Off
	for _, tg := range types {
		l3Start += 16 + 8*len(tg.names)
	}
	l3Off := l3Start
	itemIdx := 0

	for ti, tg := range types {
		writeDirEntry(16+ti*8, tg.typeID, uint32(l2Off), true)
		writeDirHeader(l2Off, len(tg.names))
		for ni, ng := range tg.names {
			writeDirEntry(l2Off+16+ni*8, ng.nameID, uint32(l3Off), true)
			writeDirHeader(l3Off, len(ng.items))
			for li, it := range ng.items {
				deOff := dataEntriesOffset + itemIdx*16
				writeDirEntry(l3Off+16+li*8, it.langID, uint32(deOff), false)

				// IMAGE_RESOURCE_DATA_ENTRY: OffsetToData (RVA via IMAGE_REL_AMD64_ADDR32NB), Size, CodePage, Reserved
				binary.LittleEndian.PutUint32(rsrc01[deOff:], dataOffsets[itemIdx])
				binary.LittleEndian.PutUint32(rsrc01[deOff+4:], uint32(len(it.data)))
				relocs = append(relocs, coffReloc{
					virtualAddr: uint32(deOff),
					symbolIndex: 1, // .rsrc$02 section symbol
					relocType:   3, // IMAGE_REL_AMD64_ADDR32NB
				})
				itemIdx++
			}
			l3Off += 16 + 8*len(ng.items)
		}
		l2Off += 16 + 8*len(tg.names)
	}

	// Build COFF file (amd64: 0x8664)
	// Header (20) + 2 Section Headers (80)
	const headerSize = 20 + 2*40
	rsrc01FileOff := headerSize
	relocsFileOff := rsrc01FileOff + len(rsrc01)
	rsrc02FileOff := relocsFileOff + len(relocs)*10
	symtabFileOff := rsrc02FileOff + rsrc02.Len()

	var out bytes.Buffer
	// IMAGE_FILE_HEADER
	binary.Write(&out, binary.LittleEndian, uint16(0x8664))        // Machine: AMD64
	binary.Write(&out, binary.LittleEndian, uint16(2))             // NumberOfSections
	binary.Write(&out, binary.LittleEndian, uint32(0))             // TimeDateStamp
	binary.Write(&out, binary.LittleEndian, uint32(symtabFileOff)) // PointerToSymbolTable
	binary.Write(&out, binary.LittleEndian, uint32(2))             // NumberOfSymbols
	binary.Write(&out, binary.LittleEndian, uint16(0))             // SizeOfOptionalHeader
	binary.Write(&out, binary.LittleEndian, uint16(0))             // Characteristics

	writeSectionHeader := func(name string, rawSize, rawPtr, relPtr uint32, nRel uint16) {
		var nameBuf [8]byte
		copy(nameBuf[:], name)
		out.Write(nameBuf[:])
		binary.Write(&out, binary.LittleEndian, uint32(0)) // VirtualSize
		binary.Write(&out, binary.LittleEndian, uint32(0)) // VirtualAddress
		binary.Write(&out, binary.LittleEndian, rawSize)
		binary.Write(&out, binary.LittleEndian, rawPtr)
		binary.Write(&out, binary.LittleEndian, relPtr)
		binary.Write(&out, binary.LittleEndian, uint32(0)) // PointerToLinenumbers
		binary.Write(&out, binary.LittleEndian, nRel)
		binary.Write(&out, binary.LittleEndian, uint16(0)) // NumberOfLinenumbers
		// IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ
		binary.Write(&out, binary.LittleEndian, uint32(0x40000040))
	}

	writeSectionHeader(".rsrc$01", uint32(len(rsrc01)), uint32(rsrc01FileOff), uint32(relocsFileOff), uint16(len(relocs)))
	writeSectionHeader(".rsrc$02", uint32(rsrc02.Len()), uint32(rsrc02FileOff), 0, 0)

	out.Write(rsrc01)
	for _, rel := range relocs {
		binary.Write(&out, binary.LittleEndian, rel.virtualAddr)
		binary.Write(&out, binary.LittleEndian, rel.symbolIndex)
		binary.Write(&out, binary.LittleEndian, rel.relocType)
	}
	out.Write(rsrc02.Bytes())

	// Symbol table (2 section symbols, 18 bytes each)
	writeSym := func(name string, secNum uint16) {
		var nameBuf [8]byte
		copy(nameBuf[:], name)
		out.Write(nameBuf[:])
		binary.Write(&out, binary.LittleEndian, uint32(0))      // Value
		binary.Write(&out, binary.LittleEndian, secNum)         // SectionNumber (1-based)
		binary.Write(&out, binary.LittleEndian, uint16(0))      // Type
		out.WriteByte(3)                                        // StorageClass: IMAGE_SYM_CLASS_STATIC
		out.WriteByte(0)                                        // NumberOfAuxSymbols
	}
	writeSym(".rsrc$01", 1)
	writeSym(".rsrc$02", 2)

	// String table size (4 bytes = 4, empty)
	binary.Write(&out, binary.LittleEndian, uint32(4))

	return out.Bytes(), nil
}
