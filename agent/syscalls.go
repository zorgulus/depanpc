package main

import (
	"syscall"
	"unsafe"
)

// Appels Win32 partagés par les commandes de diagnostic.
var kernel32 = syscall.NewLazyDLL("kernel32.dll")

var (
	procGetTickCount64      = kernel32.NewProc("GetTickCount64")
	procGetLogicalDrives    = kernel32.NewProc("GetLogicalDrives")
	procGetDriveTypeW       = kernel32.NewProc("GetDriveTypeW")
	procGetDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
	procMultiByteToWideChar = kernel32.NewProc("MultiByteToWideChar")
)

// Codepages Windows utilisées par les utilitaires console. Selon l'outil
// et selon que sa sortie est redirigée ou non, Windows choisit soit la
// codepage OEM de la console (tasklist, taskkill, ipconfig), soit la
// codepage ANSI système (wevtutil) — aucune des deux n'est UTF-8.
const (
	cpOEMCP = 1
	cpACP   = 0
)

// decodeCodepage convertit une sortie brute d'un utilitaire console
// Windows (encodée dans la codepage donnée) vers de l'UTF-8, pour éviter
// que les caractères accentués ne deviennent illisibles (ou invalides)
// une fois encodés en JSON.
func decodeCodepage(b []byte, codepage uintptr) string {
	if len(b) == 0 {
		return ""
	}
	n, _, _ := procMultiByteToWideChar.Call(
		codepage, 0,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)),
		0, 0,
	)
	if n == 0 {
		return string(b)
	}
	buf := make([]uint16, n)
	procMultiByteToWideChar.Call(
		codepage, 0,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)),
		uintptr(unsafe.Pointer(&buf[0])), n,
	)
	return syscall.UTF16ToString(buf)
}

// decodeOEM : pour tasklist, taskkill, ipconfig.
func decodeOEM(b []byte) string { return decodeCodepage(b, cpOEMCP) }

// decodeANSI : pour wevtutil, qui utilise la codepage ANSI système
// lorsque sa sortie est redirigée (comportement différent des outils
// plus anciens).
func decodeANSI(b []byte) string { return decodeCodepage(b, cpACP) }
