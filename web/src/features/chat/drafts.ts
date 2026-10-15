// Черновик сообщения (срез 35): живёт в sessionStorage вкладки, по человеку и переписке. Закрыл вкладку, и черновика нет:
// на общем компьютере недописанное письмо о вакансии не должно доставаться следующему человеку.

const key = (userId: string, chatId: string) => `scibox.chatDraft.v1:${userId}:${chatId}`

export function loadDraft(userId: string, chatId: string): string {
  try {
    return window.sessionStorage.getItem(key(userId, chatId)) ?? ''
  } catch {
    return ''
  }
}

export function saveDraft(userId: string, chatId: string, text: string) {
  try {
    if (text.trim() === '') window.sessionStorage.removeItem(key(userId, chatId))
    else window.sessionStorage.setItem(key(userId, chatId), text)
  } catch {
    // Хранилище недоступно (приватный режим): черновик живёт, пока открыта страница.
  }
}
