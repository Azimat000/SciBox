import { useMutation, useQueryClient } from '@tanstack/react-query'
import { t } from '../../i18n'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { refreshProfile, savePrivacy, visibilities, type Profile, type Visibility } from './api'
import { anonymousLabel, bestHIndex } from './labels'

type Props = {
  visibility: Visibility
  /** Как организации увидят анонимный профиль (сервер собирает его из собственных данных человека). */
  preview?: Profile
}

/** «Кто видит профиль»: четыре режима. Каждое изменение сохраняется сразу. */
export function PrivacyPanel({ visibility, preview }: Props) {
  const client = useQueryClient()
  const toast = useToast()
  const save = useMutation({
    mutationFn: (next: Visibility) => savePrivacy(next),
    onSuccess: async () => {
      await refreshProfile(client)
      toast.show({ kind: 'success', title: t.profile.privacy.saved })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.profile.privacy.saveFailed, text: describeError(err) }),
  })
  // Пока запрос идёт, показываем выбранное человеком, а не старое значение
  const shownVisibility = save.isPending ? save.variables : visibility

  return (
    <section className="privacy-panel" aria-labelledby="privacy-title">
      <h2 id="privacy-title">{t.profile.privacy.title}</h2>
      <div className="privacy-modes" role="radiogroup" aria-labelledby="privacy-title">
        {visibilities.map((mode) => (
          <label key={mode} className="privacy-mode" data-checked={shownVisibility === mode || undefined}>
            <input
              type="radio"
              name="visibility"
              value={mode}
              checked={shownVisibility === mode}
              disabled={save.isPending}
              onChange={() => save.mutate(mode)}
            />
            <span className="privacy-mode-text">
              <span className="privacy-mode-label">{t.profile.privacy.modes[mode].label}</span>
              {/* Пояснение только у выбранного режима: четыре абзаца подряд не читаются */}
              {shownVisibility === mode && <span className="privacy-mode-hint">{t.profile.privacy.modes[mode].text}</span>}
            </span>
          </label>
        ))}
      </div>
      {shownVisibility === 'anon' && preview && <AnonymousPreview preview={preview} />}
    </section>
  )
}

/** Строка «Так организации увидят вас»: то же название, что на карточке в каталоге, чтобы человек проверил её до показа. */
function AnonymousPreview({ preview }: { preview: Profile }) {
  const thin = preview.degree.level === 'none' && preview.specialties.length === 0 && bestHIndex(preview.h_index) === null
  const label = anonymousLabel({
    degree: preview.degree.level,
    title: preview.academic_title,
    codes: preview.specialties.map((s) => s.code),
    level: preview.level ?? 0,
    hIndex: bestHIndex(preview.h_index),
  })
  return (
    <p className="privacy-preview">
      <span>{t.profile.privacy.preview}</span> <strong>{label}</strong>
      {thin && <span className="field-hint">{t.profile.privacy.previewThin}</span>}
    </p>
  )
}
