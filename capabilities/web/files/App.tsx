import { useEffect, useState } from 'react'

export function App() {
  const [status, setStatus] = useState('checking...')

  useEffect(() => {
    fetch('/api/health')
      .then((response) => response.json())
      .then((data: { status: string }) => setStatus(data.status))
      .catch(() => setStatus('unreachable'))
  }, [])

  return (
    <main>
      <h1>__name__</h1>
      <p>The web capability is installed.</p>
      <p>API health: {status}</p>
    </main>
  )
}
