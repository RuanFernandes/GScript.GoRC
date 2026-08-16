import React from 'react'
import {createRoot} from 'react-dom/client'
import {loader as monacoLoader} from '@monaco-editor/react'
import './index.css'
import App from './App'
import {Toaster} from '@/components/ui/sonner'
import {useAppTheme} from '@/hooks/useAppTheme'
import {bootstrapCachedAppTheme} from '@/lib/appThemes'

monacoLoader.config({
  paths: {
    vs: 'https://cdn.jsdelivr.net/npm/monaco-editor@0.56.0/min/vs',
  },
})

// Apply the last known theme before React paints. Wails windows may not share
// localStorage, so useAppTheme still resolves the backend store and removes the
// pending gate after the authoritative theme has been applied.
bootstrapCachedAppTheme()

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
