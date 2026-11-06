/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { createInstance } from 'i18next'
import { describe, expect, test } from 'vitest'

import { resources } from '../config'
import fallbackModels from '../fallback-models.json'
import en from '../locales/en.json'
import fr from '../locales/fr.json'
import ja from '../locales/ja.json'
import ru from '../locales/ru.json'
import vi from '../locales/vi.json'
import zhTW from '../locales/zh-TW.json'
import zhCN from '../locales/zh.json'

const languages = ['en', 'zhCN', 'zhTW', 'fr', 'ja', 'ru', 'vi'] as const
const upstreamResources = { en, zhCN, zhTW, fr, ja, ru, vi }

describe('fallback model translations', () => {
  test.each(languages)(
    'loads every fallback label in %s without relying on English fallback',
    async (language) => {
      const translations = createInstance()
      await translations.init({ resources, lng: language, fallbackLng: false })

      expect(Object.keys(fallbackModels[language]).sort()).toEqual(
        Object.keys(fallbackModels.en).sort()
      )
      const upstream: Record<string, string> =
        upstreamResources[language].translation
      for (const [key, value] of Object.entries(fallbackModels[language])) {
        expect(translations.exists(key)).toBe(true)
        expect(translations.t(key)).toBe(upstream[key] ?? value)
      }
      // Shared upstream labels remain available alongside fork-only labels.
      expect(translations.exists('Save')).toBe(true)
    }
  )

  test('updates fallback labels when the interface language changes', async () => {
    const translations = createInstance()
    await translations.init({ resources, lng: 'en', fallbackLng: false })
    expect(translations.t('Fallback Models')).toBe('Fallback Models')

    await translations.changeLanguage('zhCN')
    expect(translations.t('Fallback Models')).toBe('回退模型')
    await translations.changeLanguage('zhTW')
    expect(translations.t('Fallback Models')).toBe('回退模型')
  })
})
