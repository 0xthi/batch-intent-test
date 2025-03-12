import { useState } from "react";
import { ethers } from "ethers";
import "./App.css";

declare global {
  interface Window {
    ethereum?: any;
  }
}

function App() {
  const [account, setAccount] = useState<string | null>(null);
  const [signature, setSignature] = useState<string | null>(null);
  const [tradeData, setTradeData] = useState({
    name: "ETH",
    position: "1.5",
    value: "2000",
    orderType: "BUY",
    expiry: Math.floor(Date.now() / 1000) + 86400,
  });

  async function connectWallet() {
    if (!window.ethereum) return alert("Install Metamask");
    const provider = new ethers.BrowserProvider(window.ethereum);
    const accounts: string[] = await provider.send("eth_requestAccounts", []);
    setAccount(accounts[0]);
  }

  async function signTrade() {
    if (!account) return alert("Connect Wallet First");
    const provider = new ethers.BrowserProvider(window.ethereum);
    const signer = await provider.getSigner();

    const message = JSON.stringify(tradeData);
    const signature = await signer.signMessage(message);
    setSignature(signature);
    console.log("Signed Trade:", { ...tradeData, signature });
    sendToBackend({ ...tradeData, signature });
  }

  async function sendToBackend(data: object) {
    await fetch("http://localhost:8080/store-trade", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
  }

  return (
    <div className="App">
      <h1>Trade Intent Signing</h1>
      <button onClick={connectWallet}>
        {account ? `Connected: ${account.slice(0, 6)}...${account.slice(-4)}` : "Connect Wallet"}
      </button>
      <button onClick={signTrade} className="ml-4">Sign Trade</button>
      {signature && <p className="mt-4">Signature: {signature.slice(0, 20)}...</p>}
    </div>
  );
}

export default App;