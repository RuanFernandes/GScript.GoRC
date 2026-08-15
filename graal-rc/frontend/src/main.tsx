import React from 'react'
import {createRoot} from 'react-dom/client'
import {loader as monacoLoader} from '@monaco-editor/react'
import './index.css'
import App from './App'
import {Toaster} from '@/components/ui/sonner'
import {useAppTheme} from '@/hooks/useAppTheme'

monacoLoader.config({
  paths: {
    vs: 'https://cdn.jsdelivr.net/npm/monaco-editor@0.56.0/min/vs',
  },
})

const container = document.getElementById('root')

const root = createRoot(container!)

function FrontendRoot() {
    useAppTheme()
    return <><App/><Toaster position="bottom-right" /></>
}

root.render(
    <React.StrictMode>
        <FrontendRoot/>
    </React.StrictMode>
)
