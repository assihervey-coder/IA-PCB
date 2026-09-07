/**
 * client/socket — Connexion socket.io au microservice IA (ws-client.ts).
 * Via la gateway Caddy : path "/" + XTransformPort=3010. JAMAIS de port en URL.
 */
import { io, Socket } from 'socket.io-client'

let socket: Socket | null = null

export function getAiSocket(): Socket {
  if (!socket || !socket.connected) {
    socket?.disconnect()
    socket = io('/?XTransformPort=3010', {
      path: '/',
      transports: ['websocket', 'polling'],
      reconnectionAttempts: 5,
      timeout: 8000,
    })
  }
  return socket
}

export async function waitForAiSocket(timeoutMs = 8000): Promise<Socket> {
  const s = getAiSocket()
  if (s.connected) return s
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error('Moteur IA injoignable (port 3010) — vérifiez que le mini-service est démarré.'))
    }, timeoutMs)
    s.once('connect', () => {
      clearTimeout(timer)
      resolve(s)
    })
    s.once('connect_error', () => {
      clearTimeout(timer)
      reject(new Error('Connexion au moteur IA échouée.'))
    })
  })
}
