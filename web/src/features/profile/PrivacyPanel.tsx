import { useMutation, useQueryClient } from '@tanstack/react-query'
import { t } from '../../i18n'
import { Checkbox } from '../../ui/Checkbox'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { refreshProfile, savePrivacy, visibilities, type Visibility } from './api'

type Props = { visibility: Visibility; openToOffers: boolean }

/** «Кто видит профиль»: три режима и отметка «открыт к предложениям». Каждое изменение сохраняется сразу. */
export function PrivacyPanel({ visibility, openToOffers }: Props) {
  const client = useQueryClient()
  const toast = useToast()
  const save = useMutation({
    mutationFn: (next: { visibility: Visibility; open: boolean }) => savePrivacy(next.visibility, next.open),
    onSuccess: async () => {
      await refreshProfile(client)
      toast.show({ kind: 'success', title: t.profile.privacy.saved })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.profile.privacy.saveFailed, text: describeError(err) }),
  })
  // Пока запрос идёт, показываем выбранное человеком, а не старое значение
  const shownVisibility = save.isPending ? save.variables.visibility : visibility
  const shownOffers = save.isPending ? save.variables.open : openToOffers

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
              onChange={() => save.mutate({ visibility: mode, open: shownOffers })}
            />
            <span className="privacy-mode-text">
              <span className="privacy-mode-label">{t.profile.privacy.modes[mode].label}</span>
              <span className="privacy-mode-hint">{t.profile.privacy.modes[mode].text}</span>
            </span>
          </label>
        ))}
      </div>
      <Checkbox
        label={t.profile.privacy.offers}
        checked={shownOffers}
        disabled={save.isPending}
        onChange={(e) => save.mutate({ visibility: shownVisibility, open: e.target.checked })}
      />
      <p className="field-hint">{t.profile.privacy.offersHint}</p>
    </section>
  )
}
