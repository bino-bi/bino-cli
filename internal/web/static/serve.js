var Ge=Object.defineProperty;var we=(n,e)=>()=>(n&&(e=n(n=0)),e);var Ye=(n,e)=>{for(var t in e)Ge(n,t,{get:e[t],enumerable:!0})};var Fe,je=we(()=>{Fe=(function(){"use strict";let n=()=>{},e={morphStyle:"outerHTML",callbacks:{beforeNodeAdded:n,afterNodeAdded:n,beforeNodeMorphed:n,afterNodeMorphed:n,beforeNodeRemoved:n,afterNodeRemoved:n,beforeAttributeUpdated:n},head:{style:"merge",shouldPreserve:p=>p.getAttribute("im-preserve")==="true",shouldReAppend:p=>p.getAttribute("im-re-append")==="true",shouldRemove:n,afterHeadMorphed:n},restoreFocus:!0};function t(p,_,b={}){p=w(p);let x=S(_),E=A(p,x,b),y=i(E,()=>v(E,p,x,l=>l.morphStyle==="innerHTML"?(a(l,p,x),Array.from(p.childNodes)):r(l,p,x)));return E.pantry.remove(),y}function r(p,_,b){let x=S(_);return a(p,x,b,_,_.nextSibling),Array.from(x.childNodes)}function i(p,_){if(!p.config.restoreFocus)return _();let b=document.activeElement;if(!(b instanceof HTMLInputElement||b instanceof HTMLTextAreaElement))return _();let{id:x,selectionStart:E,selectionEnd:y}=b,l=_();return x&&x!==document.activeElement?.getAttribute("id")&&(b=p.target.querySelector(`[id="${x}"]`),b?.focus()),b&&!b.selectionEnd&&y&&b.setSelectionRange(E,y),l}let a=(function(){function p(s,o,u,c=null,m=null){o instanceof HTMLTemplateElement&&u instanceof HTMLTemplateElement&&(o=o.content,u=u.content),c||=o.firstChild;for(let g of u.childNodes){if(c&&c!=m){let P=b(s,g,c,m);if(P){P!==c&&E(s,c,P),d(P,g,s),c=P.nextSibling;continue}}if(g instanceof Element){let P=g.getAttribute("id");if(s.persistentIds.has(P)){let L=y(o,P,c,s);d(L,g,s),c=L.nextSibling;continue}}let $=_(o,g,c,s);$&&(c=$.nextSibling)}for(;c&&c!=m;){let g=c;c=c.nextSibling,x(s,g)}}function _(s,o,u,c){if(c.callbacks.beforeNodeAdded(o)===!1)return null;if(c.idMap.has(o)){let m=document.createElement(o.tagName);return s.insertBefore(m,u),d(m,o,c),c.callbacks.afterNodeAdded(m),m}else{let m=document.importNode(o,!0);return s.insertBefore(m,u),c.callbacks.afterNodeAdded(m),m}}let b=(function(){function s(c,m,g,$){let P=null,L=m.nextSibling,Se=0,k=g;for(;k&&k!=$;){if(u(k,m)){if(o(c,k,m))return k;P===null&&(c.idMap.has(k)||(P=k))}if(P===null&&L&&u(k,L)&&(Se++,L=L.nextSibling,Se>=2&&(P=void 0)),c.activeElementAndParents.includes(k))break;k=k.nextSibling}return P||null}function o(c,m,g){let $=c.idMap.get(m),P=c.idMap.get(g);if(!P||!$)return!1;for(let L of $)if(P.has(L))return!0;return!1}function u(c,m){let g=c,$=m;return g.nodeType===$.nodeType&&g.tagName===$.tagName&&(!g.getAttribute?.("id")||g.getAttribute?.("id")===$.getAttribute?.("id"))}return s})();function x(s,o){if(s.idMap.has(o))h(s.pantry,o,null);else{if(s.callbacks.beforeNodeRemoved(o)===!1)return;o.parentNode?.removeChild(o),s.callbacks.afterNodeRemoved(o)}}function E(s,o,u){let c=o;for(;c&&c!==u;){let m=c;c=c.nextSibling,x(s,m)}return c}function y(s,o,u,c){let m=c.target.getAttribute?.("id")===o&&c.target||c.target.querySelector(`[id="${o}"]`)||c.pantry.querySelector(`[id="${o}"]`);return l(m,c),h(s,m,u),m}function l(s,o){let u=s.getAttribute("id");for(;s=s.parentNode;){let c=o.idMap.get(s);c&&(c.delete(u),c.size||o.idMap.delete(s))}}function h(s,o,u){if(s.moveBefore)try{s.moveBefore(o,u)}catch{s.insertBefore(o,u)}else s.insertBefore(o,u)}return p})(),d=(function(){function p(l,h,s){return s.ignoreActive&&l===document.activeElement?null:(s.callbacks.beforeNodeMorphed(l,h)===!1||(l instanceof HTMLHeadElement&&s.head.ignore||(l instanceof HTMLHeadElement&&s.head.style!=="morph"?f(l,h,s):(_(l,h,s),y(l,s)||a(s,l,h))),s.callbacks.afterNodeMorphed(l,h)),l)}function _(l,h,s){let o=h.nodeType;if(o===1){let u=l,c=h,m=u.attributes,g=c.attributes;for(let $ of g)E($.name,u,"update",s)||u.getAttribute($.name)!==$.value&&u.setAttribute($.name,$.value);for(let $=m.length-1;0<=$;$--){let P=m[$];if(P&&!c.hasAttribute(P.name)){if(E(P.name,u,"remove",s))continue;u.removeAttribute(P.name)}}y(u,s)||b(u,c,s)}(o===8||o===3)&&l.nodeValue!==h.nodeValue&&(l.nodeValue=h.nodeValue)}function b(l,h,s){if(l instanceof HTMLInputElement&&h instanceof HTMLInputElement&&h.type!=="file"){let o=h.value,u=l.value;x(l,h,"checked",s),x(l,h,"disabled",s),h.hasAttribute("value")?u!==o&&(E("value",l,"update",s)||(l.setAttribute("value",o),l.value=o)):E("value",l,"remove",s)||(l.value="",l.removeAttribute("value"))}else if(l instanceof HTMLOptionElement&&h instanceof HTMLOptionElement)x(l,h,"selected",s);else if(l instanceof HTMLTextAreaElement&&h instanceof HTMLTextAreaElement){let o=h.value,u=l.value;if(E("value",l,"update",s))return;o!==u&&(l.value=o),l.firstChild&&l.firstChild.nodeValue!==o&&(l.firstChild.nodeValue=o)}}function x(l,h,s,o){let u=h[s],c=l[s];if(u!==c){let m=E(s,l,"update",o);m||(l[s]=h[s]),u?m||l.setAttribute(s,""):E(s,l,"remove",o)||l.removeAttribute(s)}}function E(l,h,s,o){return l==="value"&&o.ignoreActiveValue&&h===document.activeElement?!0:o.callbacks.beforeAttributeUpdated(l,h,s)===!1}function y(l,h){return!!h.ignoreActiveValue&&l===document.activeElement&&l!==document.body}return p})();function v(p,_,b,x){if(p.head.block){let E=_.querySelector("head"),y=b.querySelector("head");if(E&&y){let l=f(E,y,p);return Promise.all(l).then(()=>{let h=Object.assign(p,{head:{block:!1,ignore:!0}});return x(h)})}}return x(p)}function f(p,_,b){let x=[],E=[],y=[],l=[],h=new Map;for(let o of _.children)h.set(o.outerHTML,o);for(let o of p.children){let u=h.has(o.outerHTML),c=b.head.shouldReAppend(o),m=b.head.shouldPreserve(o);u||m?c?E.push(o):(h.delete(o.outerHTML),y.push(o)):b.head.style==="append"?c&&(E.push(o),l.push(o)):b.head.shouldRemove(o)!==!1&&E.push(o)}l.push(...h.values());let s=[];for(let o of l){let u=document.createRange().createContextualFragment(o.outerHTML).firstChild;if(b.callbacks.beforeNodeAdded(u)!==!1){if("href"in u&&u.href||"src"in u&&u.src){let c,m=new Promise(function(g){c=g});u.addEventListener("load",function(){c()}),s.push(m)}p.appendChild(u),b.callbacks.afterNodeAdded(u),x.push(u)}}for(let o of E)b.callbacks.beforeNodeRemoved(o)!==!1&&(p.removeChild(o),b.callbacks.afterNodeRemoved(o));return b.head.afterHeadMorphed(p,{added:x,kept:y,removed:E}),s}let A=(function(){function p(s,o,u){let{persistentIds:c,idMap:m}=l(s,o),g=_(u),$=g.morphStyle||"outerHTML";if(!["innerHTML","outerHTML"].includes($))throw`Do not understand how to morph style ${$}`;return{target:s,newContent:o,config:g,morphStyle:$,ignoreActive:g.ignoreActive,ignoreActiveValue:g.ignoreActiveValue,restoreFocus:g.restoreFocus,idMap:m,persistentIds:c,pantry:b(),activeElementAndParents:x(s),callbacks:g.callbacks,head:g.head}}function _(s){let o=Object.assign({},e);return Object.assign(o,s),o.callbacks=Object.assign({},e.callbacks,s.callbacks),o.head=Object.assign({},e.head,s.head),o}function b(){let s=document.createElement("div");return s.hidden=!0,document.body.insertAdjacentElement("afterend",s),s}function x(s){let o=[],u=document.activeElement;if(u?.tagName!=="BODY"&&s.contains(u))for(;u&&(o.push(u),u!==s);)u=u.parentElement;return o}function E(s){let o=Array.from(s.querySelectorAll("[id]"));return s.getAttribute?.("id")&&o.push(s),o}function y(s,o,u,c){for(let m of c){let g=m.getAttribute("id");if(o.has(g)){let $=m;for(;$;){let P=s.get($);if(P==null&&(P=new Set,s.set($,P)),P.add(g),$===u)break;$=$.parentElement}}}}function l(s,o){let u=E(s),c=E(o),m=h(u,c),g=new Map;y(g,m,s,u);let $=o.__idiomorphRoot||o;return y(g,m,$,c),{persistentIds:m,idMap:g}}function h(s,o){let u=new Set,c=new Map;for(let{id:g,tagName:$}of s)c.has(g)?u.add(g):c.set(g,$);let m=new Set;for(let{id:g,tagName:$}of o)m.has(g)?u.add(g):c.get(g)===$&&m.add(g);for(let g of u)m.delete(g);return m}return p})(),{normalizeElement:w,normalizeParent:S}=(function(){let p=new WeakSet;function _(y){return y instanceof Document?y.documentElement:y}function b(y){if(y==null)return document.createElement("div");if(typeof y=="string")return b(E(y));if(p.has(y))return y;if(y instanceof Node){if(y.parentNode)return new x(y);{let l=document.createElement("div");return l.append(y),l}}else{let l=document.createElement("div");for(let h of[...y])l.append(h);return l}}class x{constructor(l){this.originalNode=l,this.realParentNode=l.parentNode,this.previousSibling=l.previousSibling,this.nextSibling=l.nextSibling}get childNodes(){let l=[],h=this.previousSibling?this.previousSibling.nextSibling:this.realParentNode.firstChild;for(;h&&h!=this.nextSibling;)l.push(h),h=h.nextSibling;return l}querySelectorAll(l){return this.childNodes.reduce((h,s)=>{if(s instanceof Element){s.matches(l)&&h.push(s);let o=s.querySelectorAll(l);for(let u=0;u<o.length;u++)h.push(o[u])}return h},[])}insertBefore(l,h){return this.realParentNode.insertBefore(l,h)}moveBefore(l,h){return this.realParentNode.moveBefore(l,h)}get __idiomorphRoot(){return this.originalNode}}function E(y){let l=new DOMParser,h=y.replace(/<svg(\s[^>]*>|>)([\s\S]*?)<\/svg>/gim,"");if(h.match(/<\/html>/)||h.match(/<\/head>/)||h.match(/<\/body>/)){let s=l.parseFromString(y,"text/html");if(h.match(/<\/html>/))return p.add(s),s;{let o=s.firstChild;return o&&p.add(o),o}}else{let o=l.parseFromString("<body><template>"+y+"</template></body>","text/html").body.querySelector("template").content;return p.add(o),o}}return{normalizeElement:_,normalizeParent:b}})();return{morph:t,defaults:e}})()});var Ke={};Ye(Ke,{appBase:()=>We,decodeBase64:()=>re,escapeHtml:()=>ut,normalizePath:()=>Ve,swapContext:()=>be,viewPath:()=>ht,waitForEngine:()=>pt});function re(n){if(!n)return"";try{return atob(n)}catch(e){return console.error("bino: decode failed",e),""}}function ut(n){var e=document.createElement("div");return e.textContent=n,e.innerHTML}function Ve(n){return n?n.charAt(0)==="/"?n:"/"+n:"/"}function We(){var n=document.querySelector("base");if(!n||!n.href)return"";var e=new URL(n.href,window.location.href).pathname;return e==="/"?"":e.replace(/\/+$/,"")}function ht(){var n=We(),e=window.location.pathname||"/";return n&&(e===n||e.indexOf(n+"/")===0)&&(e=e.slice(n.length)),Ve(e)}function pt(){return customElements.get("bn-context")?Promise.resolve():customElements.whenDefined("bn-context")}function be(n,e){if(!n)return console.debug("bino: swapContext skipped \u2014 empty html"),!1;e||(e=new DOMParser);var t=e.parseFromString(n,"text/html"),r=t.querySelector("bn-context"),i=document.querySelector("bn-context");return r?i?(ft(i,r),Fe.morph(i,r.innerHTML,{morphStyle:"innerHTML",callbacks:{beforeAttributeUpdated:function(a,d,v){if(a==="class"&&d.tagName&&d.tagName.includes("-"))return!1}}}),!0):(console.debug("bino: swapContext skipped \u2014 live DOM has no <bn-context>"),!1):(console.debug("bino: swapContext skipped \u2014 incoming HTML has no <bn-context>"),!1)}function ft(n,e){for(var t=0;t<e.attributes.length;t++){var r=e.attributes[t];r.name!=="class"&&n.getAttribute(r.name)!==r.value&&n.setAttribute(r.name,r.value)}for(var i=n.attributes.length-1;i>=0;i--){var a=n.attributes[i].name;a!=="class"&&(e.hasAttribute(a)||n.removeAttribute(a))}}var ye=we(()=>{je()});var Q=globalThis,Z=Q.ShadowRoot&&(Q.ShadyCSS===void 0||Q.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,se=Symbol(),xe=new WeakMap,F=class{constructor(e,t,r){if(this._$cssResult$=!0,r!==se)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(Z&&e===void 0){let r=t!==void 0&&t.length===1;r&&(e=xe.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),r&&xe.set(t,e))}return e}toString(){return this.cssText}},Ee=n=>new F(typeof n=="string"?n:n+"",void 0,se),j=(n,...e)=>{let t=n.length===1?n[0]:e.reduce((r,i,a)=>r+(d=>{if(d._$cssResult$===!0)return d.cssText;if(typeof d=="number")return d;throw Error("Value passed to 'css' function must be a 'css' function result: "+d+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(i)+n[a+1],n[0]);return new F(t,n,se)},Pe=(n,e)=>{if(Z)n.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let r=document.createElement("style"),i=Q.litNonce;i!==void 0&&r.setAttribute("nonce",i),r.textContent=t.cssText,n.appendChild(r)}},oe=Z?n=>n:n=>n instanceof CSSStyleSheet?(e=>{let t="";for(let r of e.cssRules)t+=r.cssText;return Ee(t)})(n):n;var{is:Qe,defineProperty:Ze,getOwnPropertyDescriptor:et,getOwnPropertyNames:tt,getOwnPropertySymbols:rt,getPrototypeOf:it}=Object,ee=globalThis,Ce=ee.trustedTypes,nt=Ce?Ce.emptyScript:"",st=ee.reactiveElementPolyfillSupport,V=(n,e)=>n,ae={toAttribute(n,e){switch(e){case Boolean:n=n?nt:null;break;case Object:case Array:n=n==null?n:JSON.stringify(n)}return n},fromAttribute(n,e){let t=n;switch(e){case Boolean:t=n!==null;break;case Number:t=n===null?null:Number(n);break;case Object:case Array:try{t=JSON.parse(n)}catch{t=null}}return t}},ke=(n,e)=>!Qe(n,e),Me={attribute:!0,type:String,converter:ae,reflect:!1,useDefault:!1,hasChanged:ke};Symbol.metadata??=Symbol("metadata"),ee.litPropertyMetadata??=new WeakMap;var O=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??=[]).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=Me){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let r=Symbol(),i=this.getPropertyDescriptor(e,r,t);i!==void 0&&Ze(this.prototype,e,i)}}static getPropertyDescriptor(e,t,r){let{get:i,set:a}=et(this.prototype,e)??{get(){return this[t]},set(d){this[t]=d}};return{get:i,set(d){let v=i?.call(this);a?.call(this,d),this.requestUpdate(e,v,r)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??Me}static _$Ei(){if(this.hasOwnProperty(V("elementProperties")))return;let e=it(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(V("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(V("properties"))){let t=this.properties,r=[...tt(t),...rt(t)];for(let i of r)this.createProperty(i,t[i])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[r,i]of t)this.elementProperties.set(r,i)}this._$Eh=new Map;for(let[t,r]of this.elementProperties){let i=this._$Eu(t,r);i!==void 0&&this._$Eh.set(i,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let r=new Set(e.flat(1/0).reverse());for(let i of r)t.unshift(oe(i))}else e!==void 0&&t.push(oe(e));return t}static _$Eu(e,t){let r=t.attribute;return r===!1?void 0:typeof r=="string"?r:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??=new Set).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let r of t.keys())this.hasOwnProperty(r)&&(e.set(r,this[r]),delete this[r]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return Pe(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,r){this._$AK(e,r)}_$ET(e,t){let r=this.constructor.elementProperties.get(e),i=this.constructor._$Eu(e,r);if(i!==void 0&&r.reflect===!0){let a=(r.converter?.toAttribute!==void 0?r.converter:ae).toAttribute(t,r.type);this._$Em=e,a==null?this.removeAttribute(i):this.setAttribute(i,a),this._$Em=null}}_$AK(e,t){let r=this.constructor,i=r._$Eh.get(e);if(i!==void 0&&this._$Em!==i){let a=r.getPropertyOptions(i),d=typeof a.converter=="function"?{fromAttribute:a.converter}:a.converter?.fromAttribute!==void 0?a.converter:ae;this._$Em=i;let v=d.fromAttribute(t,a.type);this[i]=v??this._$Ej?.get(i)??v,this._$Em=null}}requestUpdate(e,t,r,i=!1,a){if(e!==void 0){let d=this.constructor;if(i===!1&&(a=this[e]),r??=d.getPropertyOptions(e),!((r.hasChanged??ke)(a,t)||r.useDefault&&r.reflect&&a===this._$Ej?.get(e)&&!this.hasAttribute(d._$Eu(e,r))))return;this.C(e,t,r)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:r,reflect:i,wrapped:a},d){r&&!(this._$Ej??=new Map).has(e)&&(this._$Ej.set(e,d??t??this[e]),a!==!0||d!==void 0)||(this._$AL.has(e)||(this.hasUpdated||r||(t=void 0),this._$AL.set(e,t)),i===!0&&this._$Em!==e&&(this._$Eq??=new Set).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[i,a]of this._$Ep)this[i]=a;this._$Ep=void 0}let r=this.constructor.elementProperties;if(r.size>0)for(let[i,a]of r){let{wrapped:d}=a,v=this[i];d!==!0||this._$AL.has(i)||v===void 0||this.C(i,void 0,a,v)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(r=>r.hostUpdate?.()),this.update(t)):this._$EM()}catch(r){throw e=!1,this._$EM(),r}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&=this._$Eq.forEach(t=>this._$ET(t,this[t])),this._$EM()}updated(e){}firstUpdated(e){}};O.elementStyles=[],O.shadowRootOptions={mode:"open"},O[V("elementProperties")]=new Map,O[V("finalized")]=new Map,st?.({ReactiveElement:O}),(ee.reactiveElementVersions??=[]).push("2.1.2");var fe=globalThis,Te=n=>n,te=fe.trustedTypes,Le=te?te.createPolicy("lit-html",{createHTML:n=>n}):void 0,Ie="$lit$",R=`lit$${Math.random().toFixed(9).slice(2)}$`,Ue="?"+R,ot=`<${Ue}>`,N=document,K=()=>N.createComment(""),J=n=>n===null||typeof n!="object"&&typeof n!="function",me=Array.isArray,at=n=>me(n)||typeof n?.[Symbol.iterator]=="function",le=`[ 	
\f\r]`,W=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,Oe=/-->/g,Re=/>/g,q=RegExp(`>|${le}(?:([^\\s"'>=/]+)(${le}*=${le}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),qe=/'/g,He=/"/g,ze=/^(?:script|style|textarea|title)$/i,ge=n=>(e,...t)=>({_$litType$:n,strings:e,values:t}),C=ge(1),_t=ge(2),$t=ge(3),I=Symbol.for("lit-noChange"),M=Symbol.for("lit-nothing"),Ne=new WeakMap,H=N.createTreeWalker(N,129);function Be(n,e){if(!me(n)||!n.hasOwnProperty("raw"))throw Error("invalid template strings array");return Le!==void 0?Le.createHTML(e):e}var lt=(n,e)=>{let t=n.length-1,r=[],i,a=e===2?"<svg>":e===3?"<math>":"",d=W;for(let v=0;v<t;v++){let f=n[v],A,w,S=-1,p=0;for(;p<f.length&&(d.lastIndex=p,w=d.exec(f),w!==null);)p=d.lastIndex,d===W?w[1]==="!--"?d=Oe:w[1]!==void 0?d=Re:w[2]!==void 0?(ze.test(w[2])&&(i=RegExp("</"+w[2],"g")),d=q):w[3]!==void 0&&(d=q):d===q?w[0]===">"?(d=i??W,S=-1):w[1]===void 0?S=-2:(S=d.lastIndex-w[2].length,A=w[1],d=w[3]===void 0?q:w[3]==='"'?He:qe):d===He||d===qe?d=q:d===Oe||d===Re?d=W:(d=q,i=void 0);let _=d===q&&n[v+1].startsWith("/>")?" ":"";a+=d===W?f+ot:S>=0?(r.push(A),f.slice(0,S)+Ie+f.slice(S)+R+_):f+R+(S===-2?v:_)}return[Be(n,a+(n[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),r]},X=class n{constructor({strings:e,_$litType$:t},r){let i;this.parts=[];let a=0,d=0,v=e.length-1,f=this.parts,[A,w]=lt(e,t);if(this.el=n.createElement(A,r),H.currentNode=this.el.content,t===2||t===3){let S=this.el.content.firstChild;S.replaceWith(...S.childNodes)}for(;(i=H.nextNode())!==null&&f.length<v;){if(i.nodeType===1){if(i.hasAttributes())for(let S of i.getAttributeNames())if(S.endsWith(Ie)){let p=w[d++],_=i.getAttribute(S).split(R),b=/([.?@])?(.*)/.exec(p);f.push({type:1,index:a,name:b[2],strings:_,ctor:b[1]==="."?ce:b[1]==="?"?ue:b[1]==="@"?he:z}),i.removeAttribute(S)}else S.startsWith(R)&&(f.push({type:6,index:a}),i.removeAttribute(S));if(ze.test(i.tagName)){let S=i.textContent.split(R),p=S.length-1;if(p>0){i.textContent=te?te.emptyScript:"";for(let _=0;_<p;_++)i.append(S[_],K()),H.nextNode(),f.push({type:2,index:++a});i.append(S[p],K())}}}else if(i.nodeType===8)if(i.data===Ue)f.push({type:2,index:a});else{let S=-1;for(;(S=i.data.indexOf(R,S+1))!==-1;)f.push({type:7,index:a}),S+=R.length-1}a++}}static createElement(e,t){let r=N.createElement("template");return r.innerHTML=e,r}};function U(n,e,t=n,r){if(e===I)return e;let i=r!==void 0?t._$Co?.[r]:t._$Cl,a=J(e)?void 0:e._$litDirective$;return i?.constructor!==a&&(i?._$AO?.(!1),a===void 0?i=void 0:(i=new a(n),i._$AT(n,t,r)),r!==void 0?(t._$Co??=[])[r]=i:t._$Cl=i),i!==void 0&&(e=U(n,i._$AS(n,e.values),i,r)),e}var de=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:r}=this._$AD,i=(e?.creationScope??N).importNode(t,!0);H.currentNode=i;let a=H.nextNode(),d=0,v=0,f=r[0];for(;f!==void 0;){if(d===f.index){let A;f.type===2?A=new G(a,a.nextSibling,this,e):f.type===1?A=new f.ctor(a,f.name,f.strings,this,e):f.type===6&&(A=new pe(a,this,e)),this._$AV.push(A),f=r[++v]}d!==f?.index&&(a=H.nextNode(),d++)}return H.currentNode=N,i}p(e){let t=0;for(let r of this._$AV)r!==void 0&&(r.strings!==void 0?(r._$AI(e,r,t),t+=r.strings.length-2):r._$AI(e[t])),t++}},G=class n{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,r,i){this.type=2,this._$AH=M,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=r,this.options=i,this._$Cv=i?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=U(this,e,t),J(e)?e===M||e==null||e===""?(this._$AH!==M&&this._$AR(),this._$AH=M):e!==this._$AH&&e!==I&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):at(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==M&&J(this._$AH)?this._$AA.nextSibling.data=e:this.T(N.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:r}=e,i=typeof r=="number"?this._$AC(e):(r.el===void 0&&(r.el=X.createElement(Be(r.h,r.h[0]),this.options)),r);if(this._$AH?._$AD===i)this._$AH.p(t);else{let a=new de(i,this),d=a.u(this.options);a.p(t),this.T(d),this._$AH=a}}_$AC(e){let t=Ne.get(e.strings);return t===void 0&&Ne.set(e.strings,t=new X(e)),t}k(e){me(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,r,i=0;for(let a of e)i===t.length?t.push(r=new n(this.O(K()),this.O(K()),this,this.options)):r=t[i],r._$AI(a),i++;i<t.length&&(this._$AR(r&&r._$AB.nextSibling,i),t.length=i)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let r=Te(e).nextSibling;Te(e).remove(),e=r}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},z=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,r,i,a){this.type=1,this._$AH=M,this._$AN=void 0,this.element=e,this.name=t,this._$AM=i,this.options=a,r.length>2||r[0]!==""||r[1]!==""?(this._$AH=Array(r.length-1).fill(new String),this.strings=r):this._$AH=M}_$AI(e,t=this,r,i){let a=this.strings,d=!1;if(a===void 0)e=U(this,e,t,0),d=!J(e)||e!==this._$AH&&e!==I,d&&(this._$AH=e);else{let v=e,f,A;for(e=a[0],f=0;f<a.length-1;f++)A=U(this,v[r+f],t,f),A===I&&(A=this._$AH[f]),d||=!J(A)||A!==this._$AH[f],A===M?e=M:e!==M&&(e+=(A??"")+a[f+1]),this._$AH[f]=A}d&&!i&&this.j(e)}j(e){e===M?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},ce=class extends z{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===M?void 0:e}},ue=class extends z{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==M)}},he=class extends z{constructor(e,t,r,i,a){super(e,t,r,i,a),this.type=5}_$AI(e,t=this){if((e=U(this,e,t,0)??M)===I)return;let r=this._$AH,i=e===M&&r!==M||e.capture!==r.capture||e.once!==r.once||e.passive!==r.passive,a=e!==M&&(r===M||i);i&&this.element.removeEventListener(this.name,this,r),a&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},pe=class{constructor(e,t,r){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=r}get _$AU(){return this._$AM._$AU}_$AI(e){U(this,e)}};var dt=fe.litHtmlPolyfillSupport;dt?.(X,G),(fe.litHtmlVersions??=[]).push("3.3.2");var De=(n,e,t)=>{let r=t?.renderBefore??e,i=r._$litPart$;if(i===void 0){let a=t?.renderBefore??null;r._$litPart$=i=new G(e.insertBefore(K(),a),a,void 0,t??{})}return i._$AI(n),i};var ve=globalThis,T=class extends O{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let e=super.createRenderRoot();return this.renderOptions.renderBefore??=e.firstChild,e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=De(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return I}};T._$litElement$=!0,T.finalized=!0,ve.litElementHydrateSupport?.({LitElement:T});var ct=ve.litElementPolyfillSupport;ct?.({LitElement:T});(ve.litElementVersions??=[]).push("4.2.2");ye();function Je(n){var e=!1;function t(){e||(e=!0,setTimeout(function(){e=!1,r()},0))}function r(){var v=document.querySelector("bn-context");if(v){var f=getComputedStyle(v),A=v.clientWidth-parseFloat(f.paddingLeft)-parseFloat(f.paddingRight);A<=0||v.querySelectorAll(":scope > bn-layout-page").forEach(function(w){var S=w.offsetWidth,p=w.offsetHeight;if(!(S<=0)){var _=Math.min(1,A/S);w.style.setProperty("--bino-fit-scale",String(_)),w.style.setProperty("--bino-fit-w",S+"px"),w.style.setProperty("--bino-fit-h",p+"px")}})}}var i=new ResizeObserver(t);i.observe(n);var a=new ResizeObserver(t);function d(){a.disconnect(),document.querySelectorAll("bn-context > bn-layout-page").forEach(function(v){a.observe(v)}),t()}return{rebind:d,schedule:t}}var _e=class extends T{static properties={routes:{type:Object},queryParams:{type:Array},missingParams:{type:Array},currentPath:{type:String,attribute:"current-path"},mode:{type:String,reflect:!0},open:{type:Boolean,reflect:!0},_loading:{state:!0}};static styles=j`
    :host {
      width: var(--bino-sidebar-width);
      min-width: var(--bino-sidebar-width);
      background: var(--bino-surface);
      border-right: 1px solid var(--bino-border);
      padding: var(--bino-space-md);
      /* clear the shell's floating sidebar toggle */
      padding-top: calc(44px + var(--bino-space-md) + env(safe-area-inset-top, 0px));
      padding-left: calc(var(--bino-space-md) + env(safe-area-inset-left, 0px));
      padding-bottom: calc(var(--bino-space-md) + env(safe-area-inset-bottom, 0px));
      display: flex;
      flex-direction: column;
      gap: var(--bino-space-md);
      overflow-y: auto;
      overscroll-behavior: contain;
      height: 100%;
      font-family: var(--bino-font-sans);
      transition: width var(--bino-transition-normal),
        min-width var(--bino-transition-normal),
        transform var(--bino-transition-normal);
    }
    :host([mode="pinned"]:not([open])) {
      width: 0;
      min-width: 0;
      padding-left: 0;
      padding-right: 0;
      border-right: none;
      overflow: hidden;
    }
    :host([mode="drawer"]) {
      position: fixed;
      top: 0;
      bottom: 0;
      left: 0;
      box-sizing: border-box;
      width: min(320px, 85vw);
      min-width: 0;
      height: auto;
      z-index: var(--bino-z-dropdown);
      transform: translateX(-100%);
    }
    :host([mode="drawer"][open]) {
      transform: translateX(0);
      box-shadow: var(--bino-shadow-page);
    }
    :host([hidden]) {
      display: none;
    }
    h3 {
      margin: 0;
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--bino-primary);
    }
    .sitemap {
      border-bottom: 1px solid var(--bino-border);
      padding-bottom: var(--bino-space-md);
      margin-bottom: var(--bino-space-sm);
    }
    .route-list {
      list-style: none;
      margin: var(--bino-space-sm) 0 0 0;
      padding: 0;
      display: flex;
      flex-direction: column;
      gap: var(--bino-space-xs);
    }
    .route-list li a {
      display: flex;
      align-items: center;
      min-height: 44px;
      padding: var(--bino-space-sm) 0.75rem;
      border-radius: var(--bino-radius);
      text-decoration: none;
      color: var(--bino-text-muted);
      font-size: var(--bino-font-size-md);
      transition: background var(--bino-transition-fast);
      -webkit-tap-highlight-color: transparent;
    }
    .route-list li a:hover {
      background: var(--bino-surface-hover);
    }
    .route-list li.active a {
      background: var(--bino-surface-active);
      color: var(--bino-active-text);
      font-weight: 500;
    }
    .param-group {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
    }
    .param-group.missing {
      background: var(--bino-error-bg);
      border: 1px solid var(--bino-error-border);
      border-radius: var(--bino-radius);
      padding: 0.75rem;
      margin: -0.25rem;
    }
    .param-group.missing .param-label {
      color: var(--bino-error);
    }
    .param-label {
      font-size: var(--bino-font-size-base);
      font-weight: 500;
      color: var(--bino-text-muted);
    }
    .param-label .required {
      color: var(--bino-error);
      margin-left: 2px;
    }
    .param-desc {
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      margin: 0;
    }
    .param-input {
      padding: var(--bino-space-sm) 0.75rem;
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-md);
      font-family: inherit;
      transition: border-color var(--bino-transition-fast), box-shadow var(--bino-transition-fast);
    }
    .param-input:focus {
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 3px var(--bino-primary-ring);
    }
    .param-input.invalid {
      border-color: var(--bino-error);
    }
    .param-select {
      appearance: none;
      background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%236b777d' d='M3 5l3 3 3-3'/%3E%3C/svg%3E");
      background-repeat: no-repeat;
      background-position: right 0.75rem center;
      padding-right: 2rem;
      cursor: pointer;
    }
    .range-slider-container {
      display: flex;
      flex-direction: column;
      gap: var(--bino-space-sm);
    }
    .range-values {
      display: flex;
      justify-content: space-between;
      align-items: center;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text-muted);
      font-weight: 500;
    }
    .range-value {
      background: var(--bino-surface-hover);
      padding: var(--bino-space-xs) var(--bino-space-sm);
      border-radius: 4px;
      min-width: 3rem;
      text-align: center;
    }
    .range-sep {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-md);
    }
    .dual-range {
      position: relative;
      height: 1.5rem;
    }
    .range-slider {
      position: absolute;
      width: 100%;
      height: 6px;
      top: 50%;
      transform: translateY(-50%);
      -webkit-appearance: none;
      appearance: none;
      background: transparent;
      pointer-events: none;
      padding: 0;
      border: none;
      margin: 0;
    }
    .range-min { z-index: 1; }
    .range-max { z-index: 2; }
    .range-slider::-webkit-slider-runnable-track {
      width: 100%;
      height: 6px;
      background: var(--bino-border);
      border-radius: 3px;
    }
    .range-slider::-webkit-slider-thumb {
      -webkit-appearance: none;
      appearance: none;
      width: 18px;
      height: 18px;
      background: var(--bino-surface);
      border: 2px solid var(--bino-primary);
      border-radius: 50%;
      cursor: pointer;
      pointer-events: auto;
      margin-top: -6px;
      box-shadow: var(--bino-shadow-header);
    }
    .range-slider::-moz-range-track {
      width: 100%;
      height: 6px;
      background: var(--bino-border);
      border-radius: 3px;
    }
    .range-slider::-moz-range-thumb {
      width: 14px;
      height: 14px;
      background: var(--bino-surface);
      border: 2px solid var(--bino-primary);
      border-radius: 50%;
      cursor: pointer;
      pointer-events: auto;
      box-shadow: var(--bino-shadow-header);
    }
    input[type="date"].param-input,
    input[type="datetime-local"].param-input {
      cursor: pointer;
    }
    input[type="number"].param-input {
      -moz-appearance: textfield;
    }
    input[type="number"].param-input::-webkit-outer-spin-button,
    input[type="number"].param-input::-webkit-inner-spin-button {
      -webkit-appearance: none;
      margin: 0;
    }
    .apply-btn {
      padding: 0.625rem var(--bino-space-md);
      background: var(--bino-accent);
      color: var(--bino-on-accent);
      border: 1px solid var(--bino-accent-strong);
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-md);
      font-weight: 500;
      cursor: pointer;
      transition: background var(--bino-transition-fast);
    }
    .apply-btn:hover {
      background: var(--bino-accent-strong);
    }
    .apply-btn:disabled {
      background: var(--bino-gray-400);
      border-color: var(--bino-gray-400);
      cursor: not-allowed;
    }
    @media (pointer: coarse) {
      .param-input,
      .param-select {
        /* 16px prevents the iOS focus auto-zoom */
        font-size: 16px;
        min-height: 44px;
      }
      .apply-btn {
        min-height: 44px;
      }
      .range-slider::-webkit-slider-thumb {
        width: 24px;
        height: 24px;
        margin-top: -9px;
      }
      .range-slider::-moz-range-thumb {
        width: 20px;
        height: 20px;
      }
    }
    @media print {
      :host {
        display: none !important;
      }
    }
  `;constructor(){super(),this.routes={},this.queryParams=[],this.missingParams=[],this.currentPath="/",this.mode="pinned",this.open=!0,this._loading=!1}focusFirst(){var e=this.renderRoot.querySelector(".route-list a, .param-input, .apply-btn");e&&e.focus()}updateConfig(e){e.routes!==void 0&&(this.routes=e.routes),e.queryParams!==void 0&&(this.queryParams=e.queryParams),e.missingParams!==void 0&&(this.missingParams=e.missingParams),e.currentPath!==void 0&&(this.currentPath=e.currentPath)}setLoading(e){this._loading=e}render(){var e=Object.keys(this.routes).sort(),t=e.length>0,r=this.queryParams.length>0;this.hidden=!t&&!r;var i=new URLSearchParams(window.location.search);return C`
      ${t?this._renderNavigation(e):""}
      ${r?this._renderParams(i):""}
    `}_renderNavigation(e){return C`
      <div class="sitemap">
        <h3>Navigation</h3>
        <ul class="route-list">
          ${e.map(t=>{var r=this.routes[t]||t,i=t===this.currentPath;return C`
              <li class=${i?"active":""}>
                <a href=${t} @click=${a=>this._onRouteClick(a,t)}>${r}</a>
              </li>
            `})}
        </ul>
      </div>
    `}_renderParams(e){return C`
      <h3>Parameters</h3>
      ${this.queryParams.map(t=>this._renderParamGroup(t,e))}
      <button type="button" class="apply-btn"
        ?disabled=${this._loading}
        @click=${this._onApply}>
        ${this._loading?"Loading...":"Apply"}
      </button>
    `}_renderParamGroup(e,t){var r=t.get(e.name),i=null;e.type==="number_range"&&(i=t.get(e.name+"_max")),r===null&&e.default!==void 0&&e.default!==null&&(r=e.default),r=r||"",i=i||"";var a=this.missingParams.indexOf(e.name)!==-1;return C`
      <div class="param-group ${a?"missing":""}">
        <label class="param-label" for="param-${e.name}">
          ${e.name}${e.required?C`<span class="required">*</span>`:""}
        </label>
        ${e.description?C`<p class="param-desc">${e.description}</p>`:""}
        ${this._buildInput(e,r,i,a)}
      </div>
    `}_buildInput(e,t,r,i){var a=e.type||"string",d=e.options||{},v=i?" invalid":"";switch(a){case"number":return C`<input type="number"
          class="param-input${v}" id="param-${e.name}"
          name=${e.name} .value=${t}
          data-required=${e.required}
          placeholder=${e.default!=null?String(e.default):""}
          min=${d.min??""} max=${d.max??""} step=${d.step??""}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;case"number_range":{var f=d.min!==void 0?d.min:0,A=d.max!==void 0?d.max:100,w=d.step!==void 0?d.step:1,S=t!==""?parseFloat(t):f,p=r!==""?parseFloat(r):A;return C`
          <div class="range-slider-container">
            <div class="range-values">
              <span class="range-value" id="range-min-${e.name}">${S}</span>
              <span class="range-sep">\u2013</span>
              <span class="range-value" id="range-max-${e.name}">${p}</span>
            </div>
            <div class="dual-range">
              <input type="range" class="param-input range-slider range-min${v}"
                name=${e.name} .value=${String(S)}
                min=${f} max=${A} step=${w}
                data-required=${e.required}
                @input=${this._onRangeInput}>
              <input type="range" class="param-input range-slider range-max"
                name="${e.name}_max" .value=${String(p)}
                min=${f} max=${A} step=${w}
                data-required="false"
                @input=${this._onRangeInput}>
            </div>
          </div>`}case"select":return C`
          <select class="param-input param-select${v}"
            name=${e.name} data-required=${e.required}
            @keypress=${this._onKeypress} @input=${this._onInputChange}>
            ${e.required?"":C`<option value="">-- Select --</option>`}
            ${(d.items||[]).map(_=>C`
              <option value=${_.value} ?selected=${t===_.value}>
                ${_.label||_.value}
              </option>
            `)}
          </select>`;case"date":return C`<input type="date" class="param-input${v}"
          name=${e.name} .value=${t}
          data-required=${e.required}
          placeholder=${e.default!=null?String(e.default):""}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;case"date_time":return C`<input type="datetime-local" class="param-input${v}"
          name=${e.name} .value=${t}
          data-required=${e.required}
          placeholder=${e.default!=null?String(e.default):""}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;default:return C`<input type="text" class="param-input${v}"
          name=${e.name} .value=${t}
          data-required=${e.required}
          placeholder=${e.default!=null?String(e.default):""}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`}}updated(){this._setupRangeSliders()}_setupRangeSliders(){var e=this;this.renderRoot.querySelectorAll(".dual-range").forEach(function(t){var r=t.querySelector(".range-min"),i=t.querySelector(".range-max");if(!r||!i||r._rangeSetup)return;r._rangeSetup=!0;var a=e.renderRoot.getElementById("range-min-"+r.name),d=e.renderRoot.getElementById("range-max-"+i.name);function v(){var f=parseFloat(r.value),A=parseFloat(i.value);f>A&&(this===r?(r.value=A,f=A):(i.value=f,A=f)),a&&(a.textContent=r.value),d&&(d.textContent=i.value)}r.addEventListener("input",v),i.addEventListener("input",v)})}_onRouteClick(e,t){e.preventDefault(),this.dispatchEvent(new CustomEvent("bino-navigate",{detail:{path:t},bubbles:!0,composed:!0}))}_onKeypress(e){e.key==="Enter"&&this._onApply()}_onInputChange(e){var t=e.target;t.classList.remove("invalid");var r=t.closest(".param-group");r&&r.classList.remove("missing")}_onRangeInput(e){this._onInputChange(e)}_onApply(){var e=this.renderRoot.querySelectorAll(".param-input"),t=new URLSearchParams,r=!0;e.forEach(function(i){var a=i.name,d=i.value.trim(),v=i.dataset.required==="true";v&&!d?(i.classList.add("invalid"),r=!1):(i.classList.remove("invalid"),d&&t.set(a,d))}),r&&this.dispatchEvent(new CustomEvent("bino-apply-params",{detail:{params:t},bubbles:!0,composed:!0}))}};customElements.define("bino-control-panel",_e);var D=window.__binoServeConfig||{},ne=D.routes||{},ie=D.queryParams||[],B=D.missingParams||[],Y=D.currentPath||"/",Xe=D.currentURL||"/",$e=D.initialContextBase64||"",Ae=class extends T{static properties={narrow:{type:Boolean,reflect:!0},sidebarOpen:{type:Boolean,reflect:!0,attribute:"sidebar-open"},chromeless:{type:Boolean,reflect:!0}};static styles=j`
    :host {
      display: flex;
      width: 100%;
      height: 100%;
    }
    #outlet {
      flex: 1;
      min-width: 0;
      overflow: auto;
      -webkit-overflow-scrolling: touch;
      overscroll-behavior: contain;
    }
    :host([narrow][sidebar-open]) #outlet {
      overflow: hidden;
    }
    #sidebar-toggle {
      position: fixed;
      top: calc(var(--bino-space-sm) + env(safe-area-inset-top, 0px));
      left: calc(var(--bino-space-sm) + env(safe-area-inset-left, 0px));
      width: 44px;
      height: 44px;
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 0;
      border: 1px solid var(--bino-border);
      border-radius: var(--bino-radius);
      background: var(--bino-surface);
      color: var(--bino-text-muted);
      box-shadow: var(--bino-shadow-header);
      cursor: pointer;
      -webkit-tap-highlight-color: transparent;
    }
    #sidebar-toggle:hover {
      background: var(--bino-surface-hover);
    }
    :host([chromeless]) #sidebar-toggle {
      display: none;
    }
    #scrim {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-panel);
      opacity: 0;
      pointer-events: none;
      transition: opacity var(--bino-transition-normal);
    }
    :host([narrow][sidebar-open]) #scrim {
      opacity: 1;
      pointer-events: auto;
    }
    @media print {
      #sidebar-toggle,
      #scrim {
        display: none !important;
      }
    }
  `;constructor(){super(),this.narrow=!1,this.sidebarOpen=!0,this.chromeless=!1}render(){return C`
      <bino-control-panel id="panel"></bino-control-panel>
      <div id="outlet"><slot></slot></div>
      <div id="scrim" @click=${this._closeSidebar}></div>
      <button id="sidebar-toggle" type="button"
        aria-controls="panel"
        aria-expanded=${this.sidebarOpen?"true":"false"}
        aria-label=${this.sidebarOpen?"Close sidebar":"Open sidebar"}
        @click=${this._toggleSidebar}>
        ${this.sidebarOpen?C`<svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M5 5l10 10M15 5L5 15" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"/>
            </svg>`:C`<svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M3.5 5.5h13M3.5 10h13M3.5 14.5h13" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"/>
            </svg>`}
      </button>
    `}firstUpdated(){this._controlPanel=this.renderRoot.querySelector("bino-control-panel"),this._outlet=this.renderRoot.getElementById("outlet"),this._fit=Je(this._outlet),this._controlPanel&&this._controlPanel.updateConfig({routes:ne,queryParams:ie,missingParams:B,currentPath:Y}),this._syncChrome(),this._mq=window.matchMedia("(min-width: 1024px)"),this._mq.addEventListener("change",this._syncMode.bind(this)),this._syncMode(),document.addEventListener("keydown",this._onKeydown.bind(this)),this.addEventListener("bino-apply-params",this._onApplyParams.bind(this)),this.addEventListener("bino-navigate",this._onNavigate.bind(this)),this._initContent()}_syncMode(){var e=this._mq.matches;this.narrow=!e,this.sidebarOpen=e,this._controlPanel&&(this._controlPanel.mode=e?"pinned":"drawer",this._controlPanel.open=this.sidebarOpen)}_syncChrome(){this.chromeless=Object.keys(ne).length===0&&ie.length===0}_toggleSidebar(){this.sidebarOpen=!this.sidebarOpen,this._controlPanel&&(this._controlPanel.open=this.sidebarOpen,this.narrow&&this.sidebarOpen&&this._controlPanel.focusFirst())}_closeSidebar(){if(this.sidebarOpen){this.sidebarOpen=!1,this._controlPanel&&(this._controlPanel.open=!1);var e=this.renderRoot.getElementById("sidebar-toggle");e&&e.focus()}}_onKeydown(e){e.key==="Escape"&&this.narrow&&this.sidebarOpen&&this._closeSidebar()}_initContent(){if(B&&B.length>0){document.readyState==="loading"?document.addEventListener("DOMContentLoaded",this._showMissingParamsMessage.bind(this)):this._showMissingParamsMessage();return}var e=this;Promise.resolve().then(()=>(ye(),Ke)).then(function(t){t.waitForEngine().then(function(){e._injectInitialContent()})})}_injectInitialContent(){if($e){var e=re($e),t=new DOMParser;be(e,t),$e=null,this._fit.rebind()}}_showMissingParamsMessage(){var e=document.querySelector("bn-context");e&&e.remove();var t=this.querySelector(".bino-missing-params-banner");t&&t.remove();var r=document.createElement("div");r.className="bino-missing-params-banner",r.innerHTML='<div class="bino-missing-icon">\u26A0</div><div class="bino-missing-text"><strong>Required parameters missing</strong><p>Please fill in the required fields marked with <span class="required">*</span> to view the report.</p></div>',this.appendChild(r)}_onApplyParams(e){this.narrow&&this._closeSidebar();var t=e.detail.params,r=Y,i=t.toString();i&&(r+="?"+i),this._navigateTo(r)}_onNavigate(e){this.narrow&&this._closeSidebar();var t=e.detail.path;this._navigateTo(t)}_navigateTo(e){history.pushState({url:e},"",e),this._loadContent(e)}_loadContent(e){var t=this,r=document.querySelector("bn-context");r&&(r.style.opacity="0.5"),this._controlPanel&&this._controlPanel.setLoading(!0);var i=new DOMParser;fetch(e,{headers:{"X-Requested-With":"bino-serve"}}).then(function(a){if(!a.ok)throw new Error("HTTP "+a.status);return a.text()}).then(function(a){var d=i.parseFromString(a,"text/html"),v=d.getElementById("bino-serve-config");if(!v){console.error("bino: no config script found in response"),t._controlPanel&&t._controlPanel.setLoading(!1);return}var f=v.textContent,A=f.match(/window\.__binoServeConfig\s*=\s*(\{[\s\S]*\})\s*;?\s*$/),w={};if(A&&A[1])try{w=JSON.parse(A[1])}catch(o){console.error("bino: failed to parse config",o)}var S=w.missingParams||[],p=w.queryParams||[],_=w.currentPath||Y,b=w.initialContextBase64||"";B=S,ie=p,Y=_;var x=document.querySelector("bn-context");if(b){var E=re(b),y=i.parseFromString(E,"text/html"),l=y.querySelector("bn-context");if(l){if(x)x.replaceWith(l);else{var h=t.querySelector(".bino-missing-params-banner");h&&h.remove(),t.appendChild(l)}t._fit.rebind(),t._outlet.scrollTop=0}}else B.length>0&&(x&&x.remove(),t._showMissingParamsMessage(),t._outlet.scrollTop=0);var s=d.querySelector("title");s&&(document.title=s.textContent),t._controlPanel&&(t._controlPanel.updateConfig({routes:ne,queryParams:ie,missingParams:B,currentPath:Y}),t._controlPanel.setLoading(!1)),t._syncChrome()}).catch(function(a){console.error("bino: navigation failed",a),t._controlPanel&&t._controlPanel.setLoading(!1),r&&(r.style.opacity="1"),alert("Failed to load: "+a.message)})}};customElements.define("bino-serve-shell",Ae);document.addEventListener("click",function(n){var e=n.target.closest("a[href]");if(e){var t=e.getAttribute("href");if(!(!t||t.startsWith("http")||t.startsWith("//")||t.startsWith("#"))){var r=new URL(t,window.location.origin),i=r.pathname;if(ne.hasOwnProperty(i)){n.preventDefault();var a=document.querySelector("bino-serve-shell");a&&a._navigateTo(i+r.search)}}}});window.addEventListener("popstate",function(n){if(n.state&&n.state.url){var e=document.querySelector("bino-serve-shell");e&&e._loadContent(n.state.url)}});history.state||history.replaceState({url:Xe},"",Xe);
/*! Bundled license information:

@lit/reactive-element/css-tag.js:
  (**
   * @license
   * Copyright 2019 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/reactive-element.js:
lit-html/lit-html.js:
lit-element/lit-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/is-server.js:
  (**
   * @license
   * Copyright 2022 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)
*/
