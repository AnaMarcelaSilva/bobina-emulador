//go:build !linux && !windows

package janela

// Abrir ainda não tem janela nativa fora do Linux e do Windows.
func Abrir(url string, _ []byte) error { return ErrSemJanelaNativa }
