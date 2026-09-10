// Package utils — CRC-16/CCITT-FALSE.
package utils

// CRC16CCITT computes the CRC-16/CCITT-FALSE checksum mandated by the EMVCo QR
// Code specification (and therefore Indonesia's QRIS standard), and returns it
// as a 4-character uppercase hexadecimal string.
//
// Parameters: polynomial 0x1021, initial value 0xFFFF, no reflection, no final
// XOR. The checksum is defined over the payload's UTF-8 bytes.
func CRC16CCITT(input string) string {
	return CRC16CCITTBytes([]byte(input))
}

// CRC16CCITTBytes computes CRC-16/CCITT-FALSE over raw bytes.
func CRC16CCITTBytes(bytes []byte) string {
	var crc uint16 = 0xffff

	for _, b := range bytes {
		crc ^= uint16(b) << 8
		for bit := 0; bit < 8; bit++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
			crc &= 0xffff
		}
	}

	return hex4(crc)
}

func hex4(v uint16) string {
	const digits = "0123456789ABCDEF"
	b := [4]byte{
		digits[(v>>12)&0x0f],
		digits[(v>>8)&0x0f],
		digits[(v>>4)&0x0f],
		digits[v&0x0f],
	}
	return string(b[:])
}