package document

import "testing"

func TestNormalizeDocument_CPFValido(t *testing.T) {
	got, err := NormalizeDocument("111.444.777-35")
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if got != "11144477735" {
		t.Fatalf("esperava 11144477735, obteve %s", got)
	}
}

func TestNormalizeDocument_CNPJValido(t *testing.T) {
	got, err := NormalizeDocument("11.222.333/0001-81")
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if got != "11222333000181" {
		t.Fatalf("esperava 11222333000181, obteve %s", got)
	}
}

func TestNormalizeDocument_CPFInvalido(t *testing.T) {
	_, err := NormalizeDocument("111.444.777-99")
	if err != ErrInvalidDocument {
		t.Fatalf("esperava ErrInvalidDocument, obteve %v", err)
	}
}

func TestNormalizeDocument_CNPJInvalido(t *testing.T) {
	_, err := NormalizeDocument("11.222.333/0001-99")
	if err != ErrInvalidDocument {
		t.Fatalf("esperava ErrInvalidDocument, obteve %v", err)
	}
}

func TestNormalizeDocument_TodosDigitosIguais(t *testing.T) {
	_, err := NormalizeDocument("111.111.111-11")
	if err != ErrInvalidDocument {
		t.Fatalf("esperava ErrInvalidDocument, obteve %v", err)
	}
}

func TestNormalizeDocument_TamanhoInvalido(t *testing.T) {
	_, err := NormalizeDocument("123456")
	if err != ErrInvalidDocument {
		t.Fatalf("esperava ErrInvalidDocument, obteve %v", err)
	}
}

func TestNormalizeDocument_Vazio(t *testing.T) {
	_, err := NormalizeDocument("")
	if err != ErrInvalidDocument {
		t.Fatalf("esperava ErrInvalidDocument, obteve %v", err)
	}
}
