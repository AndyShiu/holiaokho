import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button, Result } from 'antd'
import { useTranslation } from 'react-i18next'

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

function Fallback({ error, onReset }: { error: Error; onReset: () => void }) {
  const { t } = useTranslation()
  return (
    <Result
      status="error"
      title={t('error.boundaryTitle', 'Something went wrong on this page')}
      subTitle={t('error.boundaryHint', 'The rest of the app still works. Reloading usually clears it; the message below helps us fix the cause.')}
      extra={[
        <Button type="primary" key="reload" onClick={() => window.location.reload()}>
          {t('error.reload', 'Reload')}
        </Button>,
        <Button key="back" onClick={onReset}>
          {t('common.back', 'Back')}
        </Button>,
      ]}
    >
      <pre className="hlk-logbox" style={{ whiteSpace: 'pre-wrap', maxHeight: 220 }}>
        {error.message}
        {error.stack ? `\n\n${error.stack.split('\n').slice(0, 6).join('\n')}` : ''}
      </pre>
    </Result>
  )
}

// A render error used to blank the whole screen, which made a small mistake
// (reading a field the API does not return) look like a dead application.
// Catch it here and keep the shell usable.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('render error', error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return <Fallback error={this.state.error} onReset={() => this.setState({ error: null })} />
    }
    return this.props.children
  }
}
