// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/settings"
)

// The words that the Go side says on its own, in the languages of the window (the same eleven as
// settings.Languages, and the same words as the page): the question of the dialog of the system
// when the page cannot ask whether to close with a queue running, and the notice of the system
// when a queue ends. Everything else the window says, the page says (frontend/i18n/).
type words struct {
	closeQuestion, closeYes, closeNo string
	// the parts of the notice, each one about a number of games, and what was saved
	done, failed, cancelled func(n int) string
	saved                   func(size string) string
	sep                     string // between the parts
}

// decimalComma are the languages that write 3,5 GB, as the page does (decimalComma in app.js)
var decimalComma = map[string]bool{"ru": true, "de": true, "es": true, "fr": true, "pt": true, "it": true}

// isolate keeps a number and its unit as left to right inside text written from right to left
// (Arabic), as the page does: 300.2 GB, not GB 300.2.
func isolate(lang, text string) string {
	if lang == "ar" {
		return "\u2066" + text + "\u2069"
	}
	return text
}

func count(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// below2 is for the languages that count 0 and 1 as singular (French)
func below2(n int, one, many string) string {
	if n < 2 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func suffix(format string) func(n int) string {
	return func(n int) string { return fmt.Sprintf(format, n) }
}

func russianPlural(n int, one, few, many string) string {
	m10, m100 := n%10, n%100
	switch {
	case m10 == 1 && m100 != 11:
		return one
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return few
	}
	return many
}

// arabicCount: one and two have a word of their own, 3 to 10 take the plural, the rest the singular
func arabicCount(n int, one, two, few, many string) string {
	switch {
	case n == 1:
		return one
	case n == 2:
		return two
	case n%100 >= 3 && n%100 <= 10:
		return fmt.Sprintf("%d %s", n, few)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func plus(text string) func(string) string { return func(size string) string { return strings.Replace(text, "%s", size, 1) } }

var wordsOf = map[string]words{
	"en": {
		"Games are at work. Cancel them and close MaxPack?", "Cancel and close", "Keep working",
		func(n int) string { return count(n, "game done", "games done") },
		suffix("%d failed"), suffix("%d cancelled"), plus("%s saved"), ", ",
	},
	"ru": {
		"Идёт работа над играми. Отменить её и закрыть MaxPack?", "Отменить и закрыть", "Продолжить работу",
		func(n int) string { return fmt.Sprintf("%d %s", n, russianPlural(n, "игра готова", "игры готовы", "игр готово")) },
		suffix("%d с ошибкой"), suffix("%d отменено"), plus("сэкономлено %s"), ", ",
	},
	"zh": {
		"游戏正在处理。要取消并关闭 MaxPack 吗？", "取消并关闭", "继续处理",
		suffix("已完成 %d 个游戏"), suffix("%d 个失败"), suffix("%d 个已取消"), plus("节省 %s"), "，",
	},
	"de": {
		"Spiele werden bearbeitet. Abbrechen und MaxPack schließen?", "Abbrechen und schließen", "Weiterarbeiten",
		func(n int) string {
			if n == 1 {
				return "1 Spiel fertig"
			}
			return fmt.Sprintf("%d Spiele fertig", n)
		},
		suffix("%d fehlgeschlagen"), suffix("%d abgebrochen"), plus("%s gespart"), ", ",
	},
	"es": {
		"Hay juegos en marcha. ¿Cancelarlos y cerrar MaxPack?", "Cancelar y cerrar", "Seguir trabajando",
		func(n int) string { return count(n, "juego listo", "juegos listos") },
		suffix("%d con error"),
		func(n int) string { return count(n, "cancelado", "cancelados") },
		plus("%s ahorrados"), ", ",
	},
	"fr": {
		"Des jeux sont en cours de traitement. Les annuler et fermer MaxPack ?", "Annuler et fermer", "Continuer le travail",
		func(n int) string { return below2(n, "jeu terminé", "jeux terminés") },
		func(n int) string { return below2(n, "échoué", "échoués") },
		func(n int) string { return below2(n, "annulé", "annulés") },
		plus("%s économisés"), ", ",
	},
	"ja": {
		"ゲームを処理中です。キャンセルして MaxPack を閉じますか？", "キャンセルして閉じる", "作業を続ける",
		suffix("%d 件完了"), suffix("%d 件失敗"), suffix("%d 件キャンセル"), plus("%s 節約"), "、",
	},
	"pt": {
		"Há jogos em andamento. Cancelá-los e fechar o MaxPack?", "Cancelar e fechar", "Continuar trabalhando",
		func(n int) string { return count(n, "jogo concluído", "jogos concluídos") },
		func(n int) string { return count(n, "falhou", "falharam") },
		func(n int) string { return count(n, "cancelado", "cancelados") },
		plus("%s economizados"), ", ",
	},
	"it": {
		"Ci sono giochi in lavorazione. Annullarli e chiudere MaxPack?", "Annulla e chiudi", "Continua a lavorare",
		func(n int) string { return count(n, "gioco completato", "giochi completati") },
		func(n int) string { return count(n, "non riuscito", "non riusciti") },
		func(n int) string { return count(n, "annullato", "annullati") },
		plus("%s risparmiati"), ", ",
	},
	"ko": {
		"게임을 처리하는 중입니다. 취소하고 MaxPack을 닫을까요?", "취소하고 닫기", "계속 작업",
		suffix("게임 %d개 완료"), suffix("%d개 실패"), suffix("%d개 취소됨"), plus("%s 절약"), ", ",
	},
	"ar": {
		"الألعاب قيد المعالجة. هل تريد إلغاءها وإغلاق MaxPack؟", "إلغاء وإغلاق", "متابعة العمل",
		func(n int) string { return "اكتمل " + arabicCount(n, "لعبة واحدة", "لعبتان", "ألعاب", "لعبة") },
		func(n int) string { return fmt.Sprintf("فشل %d", n) },
		func(n int) string { return fmt.Sprintf("أُلغي %d", n) },
		plus("تم توفير %s"), "، ",
	},
}

// wordsFor are the words of a language, English when the window has no such language.
func wordsFor(lang string) words {
	if w, ok := wordsOf[lang]; ok {
		return w
	}
	return wordsOf["en"]
}

// TellLanguage is the language the page speaks, which it tells each time it is set: the one
// chosen in Settings, or else the one of the system as the page found it. The texts the Go side
// says (the notice at the end of a queue) are in it.
func (a *App) TellLanguage(lang string) {
	if !settings.ValidLang(lang) {
		return
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.pageLang = lang
}

// language is the language of what the Go side says: the one the page told, else the one
// chosen in the settings, else the one of the system (a.lang stands in for all of it in tests).
func (a *App) language() string {
	if a.lang != nil {
		return a.lang()
	}
	a.mutex.Lock()
	told := a.pageLang
	a.mutex.Unlock()
	if told != "" {
		return told
	}
	saved, _ := settings.Load()
	return saved.EffectiveLang()
}
