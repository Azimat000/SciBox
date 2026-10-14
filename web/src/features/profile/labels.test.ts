import { describe, expect, it } from 'vitest'
import { githubLogin } from './labels'

describe('githubLogin', () => {
  it('returns the login of a saved GitHub profile link', () => {
    expect(githubLogin('https://github.com/elena-orlova')).toBe('elena-orlova')
    expect(githubLogin('https://github.com/a')).toBe('a')
    expect(githubLogin(`https://github.com/${'a'.repeat(39)}`)).toBe('a'.repeat(39))
  })

  it.each([
    '',
    'github.com/elena',
    'http://github.com/elena',
    'https://github.com/',
    'https://github.com/elena/repo',
    'https://github.com/elena/',
    'https://github.com/-elena',
    'https://github.com/ele--na',
    'https://github.com/elena-',
    `https://github.com/${'a'.repeat(40)}`,
    'https://github.com.evil.ru/elena',
    'https://github.com@evil.ru',
    'https://github.com/elena?x=1',
    'https://github.com/elena#a',
    'https://github.com/elеna',
    'javascript:alert(1)',
    ' https://github.com/elena',
    'https://github.com/elena\n',
  ])('refuses %j', (link) => {
    expect(githubLogin(link)).toBeNull()
  })
})
