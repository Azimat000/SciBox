/** Отдаёт браузеру готовый файл как скачивание. */
export function saveFile(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.append(link)
  link.click()
  link.remove()
  // Браузеру нужна секунда, чтобы начать чтение: ссылку отзываем после.
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
