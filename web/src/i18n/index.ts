// SPDX-License-Identifier: AGPL-3.0-or-later
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import en from './en.json';

// All texts of the interface come from language files (NFR-007): English
// in R1, more languages are added as files.
export const resources = { en: { translation: en } } as const;

export function initI18n(lng = 'en', extra?: Record<string, { translation: object }>) {
  void i18n.use(initReactI18next).init({
    resources: { ...resources, ...extra },
    lng,
    fallbackLng: 'en',
    interpolation: { escapeValue: false }, // React escapes
    returnNull: false,
  });
  return i18n;
}

export default i18n;
