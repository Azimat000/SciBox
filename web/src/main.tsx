import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './app/App'
import './index.css'

async function start() {
  // Подставной сервер чата (срезы 34–38): только в разработке и только с VITE_CHAT_MOCK=1. В боевой сборке
  // import.meta.env.DEV — постоянная false, условие и динамический импорт вырезаются целиком (T-104, build.test.ts).
  if (import.meta.env.DEV && import.meta.env.VITE_CHAT_MOCK === '1') {
    const { installChatMock, exposeForConsole, datasetFromUrl } = await import('./features/chat/mock')
    const data = datasetFromUrl(window.location.search)
    exposeForConsole(installChatMock({ data }).server, window.localStorage, data)
  }
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
}

void start()
