/** «ё» и «е», регистр и лишние пробелы не мешают поиску по списку. */
export function normalize(text: string): string {
  return text.toLowerCase().replaceAll('ё', 'е').trim()
}
