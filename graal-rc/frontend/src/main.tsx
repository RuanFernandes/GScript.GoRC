import React from 'react'
import {createRoot} from 'react-dom/client'
import {loader as monacoLoader} from '@monaco-editor/react'
import './index.css'
import App from './App'
import {Toaster} from '@/components/ui/sonner'

monacoLoader.config({
  paths: {
    vs: 'https://cdn.jsdelivr.net/npm/monaco-editor@0.56.0/min/vs',
  },
})

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
        <Toaster position="bottom-right" />
    </React.StrictMode>
)
