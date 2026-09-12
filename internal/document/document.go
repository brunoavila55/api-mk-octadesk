// Package document normaliza e valida CPF e CNPJ recebidos como parâmetro de entrada.
package document

import (
	"errors"
	"strconv"
	"strings"
)

var ErrInvalidDocument = errors.New("documento inválido")

// NormalizeDocument remove formatação e valida o dígito verificador de CPF (11
// dígitos) ou CNPJ (14 dígitos). Retorna apenas os dígitos, sem formatação.
func NormalizeDocument(raw string) (string, error) {
	digits := onlyDigits(raw)

	switch len(digits) {
	case 11:
		if !isValidCPF(digits) {
			return "", ErrInvalidDocument
		}
		return digits, nil
	case 14:
		if !isValidCNPJ(digits) {
			return "", ErrInvalidDocument
		}
		return digits, nil
	default:
		return "", ErrInvalidDocument
	}
}

func onlyDigits(raw string) string {
	var builder strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func allSameDigit(digits string) bool {
	for i := 1; i < len(digits); i++ {
		if digits[i] != digits[0] {
			return false
		}
	}
	return true
}

func isValidCPF(digits string) bool {
	if allSameDigit(digits) {
		return false
	}

	firstCheck := cpfCheckDigit(digits[:9], []int{10, 9, 8, 7, 6, 5, 4, 3, 2})
	secondCheck := cpfCheckDigit(digits[:9]+strconv.Itoa(firstCheck), []int{11, 10, 9, 8, 7, 6, 5, 4, 3, 2})

	return digits[9] == byte('0'+firstCheck) && digits[10] == byte('0'+secondCheck)
}

func cpfCheckDigit(base string, weights []int) int {
	sum := 0
	for i, weight := range weights {
		digit := int(base[i] - '0')
		sum += digit * weight
	}
	remainder := sum % 11
	if remainder < 2 {
		return 0
	}
	return 11 - remainder
}

func isValidCNPJ(digits string) bool {
	if allSameDigit(digits) {
		return false
	}

	firstCheck := cnpjCheckDigit(digits[:12], []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2})
	secondCheck := cnpjCheckDigit(digits[:12]+strconv.Itoa(firstCheck), []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2})

	return digits[12] == byte('0'+firstCheck) && digits[13] == byte('0'+secondCheck)
}

func cnpjCheckDigit(base string, weights []int) int {
	sum := 0
	for i, weight := range weights {
		digit := int(base[i] - '0')
		sum += digit * weight
	}
	remainder := sum % 11
	if remainder < 2 {
		return 0
	}
	return 11 - remainder
}
