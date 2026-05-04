var Me=(function(){"use strict";let o=()=>{},e={morphStyle:"outerHTML",callbacks:{beforeNodeAdded:o,afterNodeAdded:o,beforeNodeMorphed:o,afterNodeMorphed:o,beforeNodeRemoved:o,afterNodeRemoved:o,beforeAttributeUpdated:o},head:{style:"merge",shouldPreserve:f=>f.getAttribute("im-preserve")==="true",shouldReAppend:f=>f.getAttribute("im-re-append")==="true",shouldRemove:o,afterHeadMorphed:o},restoreFocus:!0};function t(f,w,g={}){f=$(f);let A=E(w),k=m(f,A,g),y=n(k,()=>h(k,f,A,d=>d.morphStyle==="innerHTML"?(i(d,f,A),Array.from(f.childNodes)):r(d,f,A)));return k.pantry.remove(),y}function r(f,w,g){let A=E(w);return i(f,A,g,w,w.nextSibling),Array.from(A.childNodes)}function n(f,w){if(!f.config.restoreFocus)return w();let g=document.activeElement;if(!(g instanceof HTMLInputElement||g instanceof HTMLTextAreaElement))return w();let{id:A,selectionStart:k,selectionEnd:y}=g,d=w();return A&&A!==document.activeElement?.getAttribute("id")&&(g=f.target.querySelector(`[id="${A}"]`),g?.focus()),g&&!g.selectionEnd&&y&&g.setSelectionRange(k,y),d}let i=(function(){function f(a,l,p,u=null,_=null){l instanceof HTMLTemplateElement&&p instanceof HTMLTemplateElement&&(l=l.content,p=p.content),u||=l.firstChild;for(let x of p.childNodes){if(u&&u!=_){let z=g(a,x,u,_);if(z){z!==u&&k(a,u,z),s(z,x,a),u=z.nextSibling;continue}}if(x instanceof Element){let z=x.getAttribute("id");if(a.persistentIds.has(z)){let T=y(l,z,u,a);s(T,x,a),u=T.nextSibling;continue}}let S=w(l,x,u,a);S&&(u=S.nextSibling)}for(;u&&u!=_;){let x=u;u=u.nextSibling,A(a,x)}}function w(a,l,p,u){if(u.callbacks.beforeNodeAdded(l)===!1)return null;if(u.idMap.has(l)){let _=document.createElement(l.tagName);return a.insertBefore(_,p),s(_,l,u),u.callbacks.afterNodeAdded(_),_}else{let _=document.importNode(l,!0);return a.insertBefore(_,p),u.callbacks.afterNodeAdded(_),_}}let g=(function(){function a(u,_,x,S){let z=null,T=_.nextSibling,Oe=0,L=x;for(;L&&L!=S;){if(p(L,_)){if(l(u,L,_))return L;z===null&&(u.idMap.has(L)||(z=L))}if(z===null&&T&&p(L,T)&&(Oe++,T=T.nextSibling,Oe>=2&&(z=void 0)),u.activeElementAndParents.includes(L))break;L=L.nextSibling}return z||null}function l(u,_,x){let S=u.idMap.get(_),z=u.idMap.get(x);if(!z||!S)return!1;for(let T of S)if(z.has(T))return!0;return!1}function p(u,_){let x=u,S=_;return x.nodeType===S.nodeType&&x.tagName===S.tagName&&(!x.getAttribute?.("id")||x.getAttribute?.("id")===S.getAttribute?.("id"))}return a})();function A(a,l){if(a.idMap.has(l))v(a.pantry,l,null);else{if(a.callbacks.beforeNodeRemoved(l)===!1)return;l.parentNode?.removeChild(l),a.callbacks.afterNodeRemoved(l)}}function k(a,l,p){let u=l;for(;u&&u!==p;){let _=u;u=u.nextSibling,A(a,_)}return u}function y(a,l,p,u){let _=u.target.getAttribute?.("id")===l&&u.target||u.target.querySelector(`[id="${l}"]`)||u.pantry.querySelector(`[id="${l}"]`);return d(_,u),v(a,_,p),_}function d(a,l){let p=a.getAttribute("id");for(;a=a.parentNode;){let u=l.idMap.get(a);u&&(u.delete(p),u.size||l.idMap.delete(a))}}function v(a,l,p){if(a.moveBefore)try{a.moveBefore(l,p)}catch{a.insertBefore(l,p)}else a.insertBefore(l,p)}return f})(),s=(function(){function f(d,v,a){return a.ignoreActive&&d===document.activeElement?null:(a.callbacks.beforeNodeMorphed(d,v)===!1||(d instanceof HTMLHeadElement&&a.head.ignore||(d instanceof HTMLHeadElement&&a.head.style!=="morph"?c(d,v,a):(w(d,v,a),y(d,a)||i(a,d,v))),a.callbacks.afterNodeMorphed(d,v)),d)}function w(d,v,a){let l=v.nodeType;if(l===1){let p=d,u=v,_=p.attributes,x=u.attributes;for(let S of x)k(S.name,p,"update",a)||p.getAttribute(S.name)!==S.value&&p.setAttribute(S.name,S.value);for(let S=_.length-1;0<=S;S--){let z=_[S];if(z&&!u.hasAttribute(z.name)){if(k(z.name,p,"remove",a))continue;p.removeAttribute(z.name)}}y(p,a)||g(p,u,a)}(l===8||l===3)&&d.nodeValue!==v.nodeValue&&(d.nodeValue=v.nodeValue)}function g(d,v,a){if(d instanceof HTMLInputElement&&v instanceof HTMLInputElement&&v.type!=="file"){let l=v.value,p=d.value;A(d,v,"checked",a),A(d,v,"disabled",a),v.hasAttribute("value")?p!==l&&(k("value",d,"update",a)||(d.setAttribute("value",l),d.value=l)):k("value",d,"remove",a)||(d.value="",d.removeAttribute("value"))}else if(d instanceof HTMLOptionElement&&v instanceof HTMLOptionElement)A(d,v,"selected",a);else if(d instanceof HTMLTextAreaElement&&v instanceof HTMLTextAreaElement){let l=v.value,p=d.value;if(k("value",d,"update",a))return;l!==p&&(d.value=l),d.firstChild&&d.firstChild.nodeValue!==l&&(d.firstChild.nodeValue=l)}}function A(d,v,a,l){let p=v[a],u=d[a];if(p!==u){let _=k(a,d,"update",l);_||(d[a]=v[a]),p?_||d.setAttribute(a,""):k(a,d,"remove",l)||d.removeAttribute(a)}}function k(d,v,a,l){return d==="value"&&l.ignoreActiveValue&&v===document.activeElement?!0:l.callbacks.beforeAttributeUpdated(d,v,a)===!1}function y(d,v){return!!v.ignoreActiveValue&&d===document.activeElement&&d!==document.body}return f})();function h(f,w,g,A){if(f.head.block){let k=w.querySelector("head"),y=g.querySelector("head");if(k&&y){let d=c(k,y,f);return Promise.all(d).then(()=>{let v=Object.assign(f,{head:{block:!1,ignore:!0}});return A(v)})}}return A(f)}function c(f,w,g){let A=[],k=[],y=[],d=[],v=new Map;for(let l of w.children)v.set(l.outerHTML,l);for(let l of f.children){let p=v.has(l.outerHTML),u=g.head.shouldReAppend(l),_=g.head.shouldPreserve(l);p||_?u?k.push(l):(v.delete(l.outerHTML),y.push(l)):g.head.style==="append"?u&&(k.push(l),d.push(l)):g.head.shouldRemove(l)!==!1&&k.push(l)}d.push(...v.values());let a=[];for(let l of d){let p=document.createRange().createContextualFragment(l.outerHTML).firstChild;if(g.callbacks.beforeNodeAdded(p)!==!1){if("href"in p&&p.href||"src"in p&&p.src){let u,_=new Promise(function(x){u=x});p.addEventListener("load",function(){u()}),a.push(_)}f.appendChild(p),g.callbacks.afterNodeAdded(p),A.push(p)}}for(let l of k)g.callbacks.beforeNodeRemoved(l)!==!1&&(f.removeChild(l),g.callbacks.afterNodeRemoved(l));return g.head.afterHeadMorphed(f,{added:A,kept:y,removed:k}),a}let m=(function(){function f(a,l,p){let{persistentIds:u,idMap:_}=d(a,l),x=w(p),S=x.morphStyle||"outerHTML";if(!["innerHTML","outerHTML"].includes(S))throw`Do not understand how to morph style ${S}`;return{target:a,newContent:l,config:x,morphStyle:S,ignoreActive:x.ignoreActive,ignoreActiveValue:x.ignoreActiveValue,restoreFocus:x.restoreFocus,idMap:_,persistentIds:u,pantry:g(),activeElementAndParents:A(a),callbacks:x.callbacks,head:x.head}}function w(a){let l=Object.assign({},e);return Object.assign(l,a),l.callbacks=Object.assign({},e.callbacks,a.callbacks),l.head=Object.assign({},e.head,a.head),l}function g(){let a=document.createElement("div");return a.hidden=!0,document.body.insertAdjacentElement("afterend",a),a}function A(a){let l=[],p=document.activeElement;if(p?.tagName!=="BODY"&&a.contains(p))for(;p&&(l.push(p),p!==a);)p=p.parentElement;return l}function k(a){let l=Array.from(a.querySelectorAll("[id]"));return a.getAttribute?.("id")&&l.push(a),l}function y(a,l,p,u){for(let _ of u){let x=_.getAttribute("id");if(l.has(x)){let S=_;for(;S;){let z=a.get(S);if(z==null&&(z=new Set,a.set(S,z)),z.add(x),S===p)break;S=S.parentElement}}}}function d(a,l){let p=k(a),u=k(l),_=v(p,u),x=new Map;y(x,_,a,p);let S=l.__idiomorphRoot||l;return y(x,_,S,u),{persistentIds:_,idMap:x}}function v(a,l){let p=new Set,u=new Map;for(let{id:x,tagName:S}of a)u.has(x)?p.add(x):u.set(x,S);let _=new Set;for(let{id:x,tagName:S}of l)_.has(x)?p.add(x):u.get(x)===S&&_.add(x);for(let x of p)_.delete(x);return _}return f})(),{normalizeElement:$,normalizeParent:E}=(function(){let f=new WeakSet;function w(y){return y instanceof Document?y.documentElement:y}function g(y){if(y==null)return document.createElement("div");if(typeof y=="string")return g(k(y));if(f.has(y))return y;if(y instanceof Node){if(y.parentNode)return new A(y);{let d=document.createElement("div");return d.append(y),d}}else{let d=document.createElement("div");for(let v of[...y])d.append(v);return d}}class A{constructor(d){this.originalNode=d,this.realParentNode=d.parentNode,this.previousSibling=d.previousSibling,this.nextSibling=d.nextSibling}get childNodes(){let d=[],v=this.previousSibling?this.previousSibling.nextSibling:this.realParentNode.firstChild;for(;v&&v!=this.nextSibling;)d.push(v),v=v.nextSibling;return d}querySelectorAll(d){return this.childNodes.reduce((v,a)=>{if(a instanceof Element){a.matches(d)&&v.push(a);let l=a.querySelectorAll(d);for(let p=0;p<l.length;p++)v.push(l[p])}return v},[])}insertBefore(d,v){return this.realParentNode.insertBefore(d,v)}moveBefore(d,v){return this.realParentNode.moveBefore(d,v)}get __idiomorphRoot(){return this.originalNode}}function k(y){let d=new DOMParser,v=y.replace(/<svg(\s[^>]*>|>)([\s\S]*?)<\/svg>/gim,"");if(v.match(/<\/html>/)||v.match(/<\/head>/)||v.match(/<\/body>/)){let a=d.parseFromString(y,"text/html");if(v.match(/<\/html>/))return f.add(a),a;{let l=a.firstChild;return l&&f.add(l),l}}else{let l=d.parseFromString("<body><template>"+y+"</template></body>","text/html").body.querySelector("template").content;return f.add(l),l}}return{normalizeElement:w,normalizeParent:g}})();return{morph:t,defaults:e}})();function j(o){return o?o.charAt(0)==="/"?o:"/"+o:"/"}function Le(){return customElements.get("bn-context")?Promise.resolve():customElements.whenDefined("bn-context")}function Te(o,e){if(!o)return console.debug("bino: swapContext skipped \u2014 empty html"),!1;e||(e=new DOMParser);var t=e.parseFromString(o,"text/html"),r=t.querySelector("bn-context"),n=document.querySelector("bn-context");return r?n?(tt(n,r),Me.morph(n,r.innerHTML,{morphStyle:"innerHTML",callbacks:{beforeAttributeUpdated:function(i,s,h){if(i==="class"&&s.tagName&&s.tagName.includes("-"))return!1}}}),!0):(console.debug("bino: swapContext skipped \u2014 live DOM has no <bn-context>"),!1):(console.debug("bino: swapContext skipped \u2014 incoming HTML has no <bn-context>"),!1)}function tt(o,e){for(var t=0;t<e.attributes.length;t++){var r=e.attributes[t];r.name!=="class"&&o.getAttribute(r.name)!==r.value&&o.setAttribute(r.name,r.value)}for(var n=o.attributes.length-1;n>=0;n--){var i=o.attributes[n].name;i!=="class"&&(e.hasAttribute(i)||o.removeAttribute(i))}}var ee=globalThis,te=ee.ShadowRoot&&(ee.ShadyCSS===void 0||ee.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,se=Symbol(),Re=new WeakMap,F=class{constructor(e,t,r){if(this._$cssResult$=!0,r!==se)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(te&&e===void 0){let r=t!==void 0&&t.length===1;r&&(e=Re.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),r&&Re.set(t,e))}return e}toString(){return this.cssText}},Ne=o=>new F(typeof o=="string"?o:o+"",void 0,se),M=(o,...e)=>{let t=o.length===1?o[0]:e.reduce((r,n,i)=>r+(s=>{if(s._$cssResult$===!0)return s.cssText;if(typeof s=="number")return s;throw Error("Value passed to 'css' function must be a 'css' function result: "+s+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(n)+o[i+1],o[0]);return new F(t,o,se)},De=(o,e)=>{if(te)o.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let r=document.createElement("style"),n=ee.litNonce;n!==void 0&&r.setAttribute("nonce",n),r.textContent=t.cssText,o.appendChild(r)}},oe=te?o=>o:o=>o instanceof CSSStyleSheet?(e=>{let t="";for(let r of e.cssRules)t+=r.cssText;return Ne(t)})(o):o;var{is:rt,defineProperty:nt,getOwnPropertyDescriptor:it,getOwnPropertyNames:st,getOwnPropertySymbols:ot,getPrototypeOf:at}=Object,re=globalThis,Pe=re.trustedTypes,lt=Pe?Pe.emptyScript:"",dt=re.reactiveElementPolyfillSupport,W=(o,e)=>o,ae={toAttribute(o,e){switch(e){case Boolean:o=o?lt:null;break;case Object:case Array:o=o==null?o:JSON.stringify(o)}return o},fromAttribute(o,e){let t=o;switch(e){case Boolean:t=o!==null;break;case Number:t=o===null?null:Number(o);break;case Object:case Array:try{t=JSON.parse(o)}catch{t=null}}return t}},Ie=(o,e)=>!rt(o,e),He={attribute:!0,type:String,converter:ae,reflect:!1,useDefault:!1,hasChanged:Ie};Symbol.metadata??=Symbol("metadata"),re.litPropertyMetadata??=new WeakMap;var R=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??=[]).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=He){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let r=Symbol(),n=this.getPropertyDescriptor(e,r,t);n!==void 0&&nt(this.prototype,e,n)}}static getPropertyDescriptor(e,t,r){let{get:n,set:i}=it(this.prototype,e)??{get(){return this[t]},set(s){this[t]=s}};return{get:n,set(s){let h=n?.call(this);i?.call(this,s),this.requestUpdate(e,h,r)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??He}static _$Ei(){if(this.hasOwnProperty(W("elementProperties")))return;let e=at(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(W("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(W("properties"))){let t=this.properties,r=[...st(t),...ot(t)];for(let n of r)this.createProperty(n,t[n])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[r,n]of t)this.elementProperties.set(r,n)}this._$Eh=new Map;for(let[t,r]of this.elementProperties){let n=this._$Eu(t,r);n!==void 0&&this._$Eh.set(n,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let r=new Set(e.flat(1/0).reverse());for(let n of r)t.unshift(oe(n))}else e!==void 0&&t.push(oe(e));return t}static _$Eu(e,t){let r=t.attribute;return r===!1?void 0:typeof r=="string"?r:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??=new Set).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let r of t.keys())this.hasOwnProperty(r)&&(e.set(r,this[r]),delete this[r]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return De(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,r){this._$AK(e,r)}_$ET(e,t){let r=this.constructor.elementProperties.get(e),n=this.constructor._$Eu(e,r);if(n!==void 0&&r.reflect===!0){let i=(r.converter?.toAttribute!==void 0?r.converter:ae).toAttribute(t,r.type);this._$Em=e,i==null?this.removeAttribute(n):this.setAttribute(n,i),this._$Em=null}}_$AK(e,t){let r=this.constructor,n=r._$Eh.get(e);if(n!==void 0&&this._$Em!==n){let i=r.getPropertyOptions(n),s=typeof i.converter=="function"?{fromAttribute:i.converter}:i.converter?.fromAttribute!==void 0?i.converter:ae;this._$Em=n;let h=s.fromAttribute(t,i.type);this[n]=h??this._$Ej?.get(n)??h,this._$Em=null}}requestUpdate(e,t,r,n=!1,i){if(e!==void 0){let s=this.constructor;if(n===!1&&(i=this[e]),r??=s.getPropertyOptions(e),!((r.hasChanged??Ie)(i,t)||r.useDefault&&r.reflect&&i===this._$Ej?.get(e)&&!this.hasAttribute(s._$Eu(e,r))))return;this.C(e,t,r)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:r,reflect:n,wrapped:i},s){r&&!(this._$Ej??=new Map).has(e)&&(this._$Ej.set(e,s??t??this[e]),i!==!0||s!==void 0)||(this._$AL.has(e)||(this.hasUpdated||r||(t=void 0),this._$AL.set(e,t)),n===!0&&this._$Em!==e&&(this._$Eq??=new Set).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[n,i]of this._$Ep)this[n]=i;this._$Ep=void 0}let r=this.constructor.elementProperties;if(r.size>0)for(let[n,i]of r){let{wrapped:s}=i,h=this[n];s!==!0||this._$AL.has(n)||h===void 0||this.C(n,void 0,i,h)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(r=>r.hostUpdate?.()),this.update(t)):this._$EM()}catch(r){throw e=!1,this._$EM(),r}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&=this._$Eq.forEach(t=>this._$ET(t,this[t])),this._$EM()}updated(e){}firstUpdated(e){}};R.elementStyles=[],R.shadowRootOptions={mode:"open"},R[W("elementProperties")]=new Map,R[W("finalized")]=new Map,dt?.({ReactiveElement:R}),(re.reactiveElementVersions??=[]).push("2.1.2");var fe=globalThis,Ue=o=>o,ne=fe.trustedTypes,qe=ne?ne.createPolicy("lit-html",{createHTML:o=>o}):void 0,We="$lit$",D=`lit$${Math.random().toFixed(9).slice(2)}$`,Qe="?"+D,ct=`<${Qe}>`,I=document,J=()=>I.createComment(""),G=o=>o===null||typeof o!="object"&&typeof o!="function",be=Array.isArray,ht=o=>be(o)||typeof o?.[Symbol.iterator]=="function",le=`[ 	
\f\r]`,Q=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,Be=/-->/g,Ke=/>/g,P=RegExp(`>|${le}(?:([^\\s"'>=/]+)(${le}*=${le}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),Ve=/'/g,je=/"/g,Je=/^(?:script|style|textarea|title)$/i,ve=o=>(e,...t)=>({_$litType$:o,strings:e,values:t}),b=ve(1),me=ve(2),Ot=ve(3),U=Symbol.for("lit-noChange"),C=Symbol.for("lit-nothing"),Fe=new WeakMap,H=I.createTreeWalker(I,129);function Ge(o,e){if(!be(o)||!o.hasOwnProperty("raw"))throw Error("invalid template strings array");return qe!==void 0?qe.createHTML(e):e}var ut=(o,e)=>{let t=o.length-1,r=[],n,i=e===2?"<svg>":e===3?"<math>":"",s=Q;for(let h=0;h<t;h++){let c=o[h],m,$,E=-1,f=0;for(;f<c.length&&(s.lastIndex=f,$=s.exec(c),$!==null);)f=s.lastIndex,s===Q?$[1]==="!--"?s=Be:$[1]!==void 0?s=Ke:$[2]!==void 0?(Je.test($[2])&&(n=RegExp("</"+$[2],"g")),s=P):$[3]!==void 0&&(s=P):s===P?$[0]===">"?(s=n??Q,E=-1):$[1]===void 0?E=-2:(E=s.lastIndex-$[2].length,m=$[1],s=$[3]===void 0?P:$[3]==='"'?je:Ve):s===je||s===Ve?s=P:s===Be||s===Ke?s=Q:(s=P,n=void 0);let w=s===P&&o[h+1].startsWith("/>")?" ":"";i+=s===Q?c+ct:E>=0?(r.push(m),c.slice(0,E)+We+c.slice(E)+D+w):c+D+(E===-2?h:w)}return[Ge(o,i+(o[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),r]},Y=class o{constructor({strings:e,_$litType$:t},r){let n;this.parts=[];let i=0,s=0,h=e.length-1,c=this.parts,[m,$]=ut(e,t);if(this.el=o.createElement(m,r),H.currentNode=this.el.content,t===2||t===3){let E=this.el.content.firstChild;E.replaceWith(...E.childNodes)}for(;(n=H.nextNode())!==null&&c.length<h;){if(n.nodeType===1){if(n.hasAttributes())for(let E of n.getAttributeNames())if(E.endsWith(We)){let f=$[s++],w=n.getAttribute(E).split(D),g=/([.?@])?(.*)/.exec(f);c.push({type:1,index:i,name:g[2],strings:w,ctor:g[1]==="."?ce:g[1]==="?"?he:g[1]==="@"?ue:V}),n.removeAttribute(E)}else E.startsWith(D)&&(c.push({type:6,index:i}),n.removeAttribute(E));if(Je.test(n.tagName)){let E=n.textContent.split(D),f=E.length-1;if(f>0){n.textContent=ne?ne.emptyScript:"";for(let w=0;w<f;w++)n.append(E[w],J()),H.nextNode(),c.push({type:2,index:++i});n.append(E[f],J())}}}else if(n.nodeType===8)if(n.data===Qe)c.push({type:2,index:i});else{let E=-1;for(;(E=n.data.indexOf(D,E+1))!==-1;)c.push({type:7,index:i}),E+=D.length-1}i++}}static createElement(e,t){let r=I.createElement("template");return r.innerHTML=e,r}};function K(o,e,t=o,r){if(e===U)return e;let n=r!==void 0?t._$Co?.[r]:t._$Cl,i=G(e)?void 0:e._$litDirective$;return n?.constructor!==i&&(n?._$AO?.(!1),i===void 0?n=void 0:(n=new i(o),n._$AT(o,t,r)),r!==void 0?(t._$Co??=[])[r]=n:t._$Cl=n),n!==void 0&&(e=K(o,n._$AS(o,e.values),n,r)),e}var de=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:r}=this._$AD,n=(e?.creationScope??I).importNode(t,!0);H.currentNode=n;let i=H.nextNode(),s=0,h=0,c=r[0];for(;c!==void 0;){if(s===c.index){let m;c.type===2?m=new X(i,i.nextSibling,this,e):c.type===1?m=new c.ctor(i,c.name,c.strings,this,e):c.type===6&&(m=new pe(i,this,e)),this._$AV.push(m),c=r[++h]}s!==c?.index&&(i=H.nextNode(),s++)}return H.currentNode=I,n}p(e){let t=0;for(let r of this._$AV)r!==void 0&&(r.strings!==void 0?(r._$AI(e,r,t),t+=r.strings.length-2):r._$AI(e[t])),t++}},X=class o{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,r,n){this.type=2,this._$AH=C,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=r,this.options=n,this._$Cv=n?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=K(this,e,t),G(e)?e===C||e==null||e===""?(this._$AH!==C&&this._$AR(),this._$AH=C):e!==this._$AH&&e!==U&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):ht(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==C&&G(this._$AH)?this._$AA.nextSibling.data=e:this.T(I.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:r}=e,n=typeof r=="number"?this._$AC(e):(r.el===void 0&&(r.el=Y.createElement(Ge(r.h,r.h[0]),this.options)),r);if(this._$AH?._$AD===n)this._$AH.p(t);else{let i=new de(n,this),s=i.u(this.options);i.p(t),this.T(s),this._$AH=i}}_$AC(e){let t=Fe.get(e.strings);return t===void 0&&Fe.set(e.strings,t=new Y(e)),t}k(e){be(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,r,n=0;for(let i of e)n===t.length?t.push(r=new o(this.O(J()),this.O(J()),this,this.options)):r=t[n],r._$AI(i),n++;n<t.length&&(this._$AR(r&&r._$AB.nextSibling,n),t.length=n)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let r=Ue(e).nextSibling;Ue(e).remove(),e=r}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},V=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,r,n,i){this.type=1,this._$AH=C,this._$AN=void 0,this.element=e,this.name=t,this._$AM=n,this.options=i,r.length>2||r[0]!==""||r[1]!==""?(this._$AH=Array(r.length-1).fill(new String),this.strings=r):this._$AH=C}_$AI(e,t=this,r,n){let i=this.strings,s=!1;if(i===void 0)e=K(this,e,t,0),s=!G(e)||e!==this._$AH&&e!==U,s&&(this._$AH=e);else{let h=e,c,m;for(e=i[0],c=0;c<i.length-1;c++)m=K(this,h[r+c],t,c),m===U&&(m=this._$AH[c]),s||=!G(m)||m!==this._$AH[c],m===C?e=C:e!==C&&(e+=(m??"")+i[c+1]),this._$AH[c]=m}s&&!n&&this.j(e)}j(e){e===C?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},ce=class extends V{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===C?void 0:e}},he=class extends V{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==C)}},ue=class extends V{constructor(e,t,r,n,i){super(e,t,r,n,i),this.type=5}_$AI(e,t=this){if((e=K(this,e,t,0)??C)===U)return;let r=this._$AH,n=e===C&&r!==C||e.capture!==r.capture||e.once!==r.once||e.passive!==r.passive,i=e!==C&&(r===C||n);n&&this.element.removeEventListener(this.name,this,r),i&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},pe=class{constructor(e,t,r){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=r}get _$AU(){return this._$AM._$AU}_$AI(e){K(this,e)}};var pt=fe.litHtmlPolyfillSupport;pt?.(Y,X),(fe.litHtmlVersions??=[]).push("3.3.2");var Ye=(o,e,t)=>{let r=t?.renderBefore??e,n=r._$litPart$;if(n===void 0){let i=t?.renderBefore??null;r._$litPart$=n=new X(e.insertBefore(J(),i),i,void 0,t??{})}return n._$AI(o),n};var ge=globalThis,O=class extends R{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let e=super.createRenderRoot();return this.renderOptions.renderBefore??=e.firstChild,e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=Ye(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return U}};O._$litElement$=!0,O.finalized=!0,ge.litElementHydrateSupport?.({LitElement:O});var ft=ge.litElementPolyfillSupport;ft?.({LitElement:O});(ge.litElementVersions??=[]).push("4.2.2");var _e=class extends O{static properties={artifacts:{type:Array},documents:{type:Array},graph:{type:Object},currentPath:{type:String,attribute:"current-path"},_errorCount:{state:!0},_badgeVisible:{state:!0},_refreshing:{state:!0},_refreshError:{state:!0}};static styles=M`
    :host {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      z-index: var(--bino-z-toolbar);
      display: flex;
      align-items: center;
      gap: var(--bino-space-md);
      background: var(--bino-surface);
      border-bottom: 1px solid var(--bino-border);
      padding: var(--bino-space-sm) var(--bino-space-md);
      font-size: var(--bino-font-size-md);
      font-family: var(--bino-font-sans);
      box-shadow: var(--bino-shadow-header);
    }
    .title {
      font-weight: 600;
      color: var(--bino-text-muted);
    }
    select {
      padding: 0.375rem 0.625rem;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: #f9fafb;
      font-size: var(--bino-font-size-md);
      color: var(--bino-text-muted);
      cursor: pointer;
      min-width: var(--bino-search-width);
    }
    select:hover {
      border-color: #9ca3af;
    }
    select:focus {
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
    }
    .warning-badge {
      display: none;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-warning-bg);
      border: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      cursor: pointer;
      user-select: none;
    }
    .warning-badge:hover {
      background: #fef3c7;
    }
    .warning-badge.visible {
      display: inline-flex;
    }
    .warning-icon {
      font-size: var(--bino-font-size-md);
    }
    .assets-btn, .graph-btn, .explorer-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border-light);
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .assets-btn:hover, .graph-btn:hover, .explorer-btn:hover {
      background: var(--bino-surface-hover);
      border-color: #9ca3af;
    }
    .assets-icon, .graph-icon, .explorer-icon {
      font-size: var(--bino-font-size-md);
    }
    .present-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: #22c55e;
      border: 1px solid #16a34a;
      color: #fff;
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .present-btn:hover {
      background: #16a34a;
    }
    .present-icon {
      font-size: var(--bino-font-size-md);
    }
    .spacer {
      flex: 1;
    }
    ::slotted(*) {
      margin-left: auto;
    }
    .progress-bar {
      position: absolute;
      bottom: 0;
      left: 0;
      right: 0;
      height: 2px;
      overflow: hidden;
      opacity: 0;
      transition: opacity 0.15s ease;
    }
    .progress-bar.active {
      opacity: 1;
    }
    .progress-bar::after {
      content: '';
      display: block;
      height: 100%;
      width: 40%;
      background: var(--bino-primary);
      border-radius: 1px;
      animation: progress-slide 1.2s ease-in-out infinite;
    }
    .progress-bar.error {
      opacity: 1;
      height: 3px;
    }
    .progress-bar.error::after {
      width: 100%;
      background: #dc2626;
      animation: none;
    }
    @keyframes progress-slide {
      0% { transform: translateX(-100%); }
      100% { transform: translateX(350%); }
    }
    .refresh-error-msg {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: #fee2e2;
      border: 1px solid #fca5a5;
      color: #991b1b;
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      max-width: 32rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      cursor: help;
    }
  `;constructor(){super(),this.artifacts=[],this.documents=[],this.graph=null,this.currentPath="/",this._errorCount=0,this._badgeVisible=!1,this._refreshing=!1,this._refreshError="",this._panelDismissed=!1,this._boundOnErrorsChanged=this._onErrorsChanged.bind(this),this._boundOnPanelDismissed=this._onPanelDismissed.bind(this),this._boundOnRefreshing=this._onRefreshing.bind(this),this._boundOnRefreshDone=this._onRefreshDone.bind(this),this._boundOnRefreshError=this._onRefreshError.bind(this),this._boundOnNoPayload=this._onNoPayload.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-errors-changed",this._boundOnErrorsChanged),document.addEventListener("bino-panel-dismissed",this._boundOnPanelDismissed),document.addEventListener("bn-preview:refreshing",this._boundOnRefreshing),document.addEventListener("bn-preview:refresh-done",this._boundOnRefreshDone),document.addEventListener("bn-preview:refresh-error",this._boundOnRefreshError),document.addEventListener("bn-preview:no-payload",this._boundOnNoPayload)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-errors-changed",this._boundOnErrorsChanged),document.removeEventListener("bino-panel-dismissed",this._boundOnPanelDismissed),document.removeEventListener("bn-preview:refreshing",this._boundOnRefreshing),document.removeEventListener("bn-preview:refresh-done",this._boundOnRefreshDone),document.removeEventListener("bn-preview:refresh-error",this._boundOnRefreshError),document.removeEventListener("bn-preview:no-payload",this._boundOnNoPayload)}render(){var e=this,t=this.currentPath||"/",r=this.artifacts||[],n=[],i=[];r.forEach(function(c){c.isDoc?i.push(c):n.push(c)});var s=t!=="/"&&!t.startsWith("/doc/")&&!t.startsWith("/pres/"),h=s?"/pres"+t:null;return b`
      <span class="title">bino preview</span>
      <select id="artefact-select" @change=${this._onSelectChange}>
        <option value="/" ?selected=${t==="/"}>All Pages</option>
        ${n.length>0?b`
          <optgroup label="Report Artefacts">
            ${n.map(function(c){var m="/"+c.name,$=c.title?c.title+" ("+c.name+")":c.name;return b`<option value=${m} ?selected=${m===t}>${$}</option>`})}
          </optgroup>
        `:""}
        ${i.length>0?b`
          <optgroup label="Document Artefacts">
            ${i.map(function(c){var m="/doc/"+c.name,$=c.title?c.title+" ("+c.name+")":c.name;return b`<option value=${m} ?selected=${m===t}>${$}</option>`})}
          </optgroup>
        `:""}
      </select>
      <span class="warning-badge ${this._badgeVisible?"visible":""}"
        title="Show warnings" @click=${this._onBadgeClick}>
        <span class="warning-icon">\u26A0</span>
        <span>${this._errorCount}</span>
      </span>
      <button class="assets-btn" title="Manifest documents" @click=${this._onAssetsClick}>
        <span class="assets-icon">\u25A6</span>
        <span>Assets (${(this.documents||[]).length})</span>
      </button>
      ${this.graph?b`
        <button class="graph-btn" title="Dependency graph" @click=${this._onGraphClick}>
          <span class="graph-icon">\u229E</span>
          <span>Graph</span>
        </button>
      `:""}
      <button class="explorer-btn" title="Data Explorer" @click=${this._onExplorerClick}>
        <span class="explorer-icon">\u2636</span>
        <span>Explorer</span>
      </button>
      ${h?b`
        <button class="present-btn" title="Open presentation" @click=${function(){window.open(h,"_blank")}}>
          <span class="present-icon">\u25B6</span>
          <span>Present</span>
        </button>
      `:""}
      <span class="spacer"></span>
      ${this._refreshError?b`
        <span class="refresh-error-msg" title=${this._refreshError}>
          <span>⚠</span>
          <span>Refresh failed</span>
        </span>
      `:""}
      <slot></slot>
      <div class="progress-bar ${this._refreshError?"error":this._refreshing?"active":""}"></div>
    `}updated(e){e.has("documents")&&document.dispatchEvent(new CustomEvent("bino-documents-changed",{detail:{documents:this.documents||[]}}))}_onAssetsClick(){document.dispatchEvent(new CustomEvent("bino-open-assets",{detail:{documents:this.documents||[]}}))}_onGraphClick(){document.dispatchEvent(new CustomEvent("bino-open-graph",{detail:{graph:this.graph}}))}_onExplorerClick(){document.dispatchEvent(new CustomEvent("bino-open-explorer"))}_onSelectChange(e){var t=e.target.value;t&&(window.location.href=t)}_onBadgeClick(){this._panelDismissed=!1,this._badgeVisible=!1,document.dispatchEvent(new CustomEvent("bino-show-errors"))}_onErrorsChanged(e){this._errorCount=e.detail&&e.detail.count||0,this._panelDismissed&&this._errorCount>0?this._badgeVisible=!0:this._badgeVisible=!1}_onPanelDismissed(){this._panelDismissed=!0,this._errorCount>0&&(this._badgeVisible=!0)}_onRefreshing(){console.debug("bino-toolbar: refreshing \u2192 _refreshing=true"),this._refreshing=!0,this._refreshError=""}_onRefreshDone(){console.debug("bino-toolbar: refresh-done \u2192 _refreshing=false"),this._refreshing=!1}_onRefreshError(e){var t=e&&e.detail&&e.detail.message||"Refresh failed";console.debug("bino-toolbar: refresh-error",t),this._refreshError=String(t),this._refreshing=!1}_onNoPayload(e){if(!this._refreshError){var t=e&&e.detail&&e.detail.path||"";this._refreshError="No content was broadcast for "+t+'. Check the bino terminal for "Render blocked" or "Render failed" messages.',this._refreshing=!1}}};customElements.define("bino-toolbar",_e);var ye=class extends O{static properties={_errors:{state:!0},_visible:{state:!0}};static styles=M`
    :host {
      position: fixed;
      bottom: 0;
      left: 0;
      right: 0;
      max-height: var(--bino-panel-max-height);
      overflow-y: auto;
      background: var(--bino-warning-bg);
      border-top: 2px solid var(--bino-warning);
      font-family: var(--bino-font-sans);
      font-size: 13px;
      z-index: var(--bino-z-panel);
      box-shadow: var(--bino-shadow-panel);
      display: none;
    }
    :host([visible]) {
      display: block;
    }
    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 8px 12px;
      background: #fef3c7;
      border-bottom: 1px solid var(--bino-warning-border);
      font-weight: 600;
      color: var(--bino-warning-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 18px;
      cursor: pointer;
      color: var(--bino-warning-text);
      padding: 0 4px;
    }
    .close-btn:hover {
      color: #78350f;
    }
    ul {
      list-style: none;
      margin: 0;
      padding: 0;
    }
    li {
      padding: 8px 12px;
      border-bottom: 1px solid #fde68a;
      cursor: pointer;
      display: flex;
      align-items: flex-start;
      gap: 8px;
    }
    li:hover {
      background: #fef3c7;
    }
    li.highlighted {
      background: #fde68a;
      border-left: 3px solid var(--bino-warning);
    }
    li:last-child {
      border-bottom: none;
    }
    .badge {
      flex-shrink: 0;
      padding: 2px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge.warning {
      background: var(--bino-warning-border);
      color: #78350f;
    }
    .badge.error {
      background: #fca5a5;
      color: #7f1d1d;
    }
    .message {
      color: #78350f;
    }
  `;constructor(){super(),this._errors=[],this._visible=!1,this._scanTimer=null,this._observer=null,this._badges=[],this._highlightTimer=null,this._boundOnShowErrors=this._onShowErrors.bind(this),this._boundOnContentUpdated=this._debouncedScan.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-show-errors",this._boundOnShowErrors),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._startObserver(),this._debouncedScan()}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-show-errors",this._boundOnShowErrors),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._observer&&(this._observer.disconnect(),this._observer=null),this._removeBadges()}updated(e){e.has("_visible")&&(this._visible?this.setAttribute("visible",""):this.removeAttribute("visible"))}render(){if(!this._visible||this._errors.length===0)return b``;var e=this,t=this._errors.length;return b`
      <div class="header">
        <span>${t} warning${t!==1?"s":""} found</span>
        <button class="close-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <ul>
        ${this._errors.map(function(r,n){return b`
            <li @click=${()=>e._scrollToElement(r.element)}>
              <span class="badge ${r.error.type||"warning"}">${r.error.type||"warning"}</span>
              <span class="message">${r.error.message||r.error.id||"Unknown error"}</span>
            </li>
          `})}
      </ul>
    `}_startObserver(){var e=this;this._observer=new MutationObserver(function(t){e._onMutation(t)}),this._observer.observe(document.body,{childList:!0,subtree:!0,attributes:!0,attributeFilter:["has-error","has-errors"]})}_onMutation(e){var t=!1;e.forEach(function(r){r.type==="attributes"&&(r.attributeName==="has-error"||r.attributeName==="has-errors")&&(t=!0),r.type==="childList"&&r.addedNodes.length>0&&r.addedNodes.forEach(function(n){n.nodeType===1&&n.hasAttribute&&(n.hasAttribute("has-error")||n.hasAttribute("has-errors"))&&(t=!0),n.nodeType===1&&n.querySelector&&n.querySelector("[has-error], [has-errors]")&&(t=!0)})}),t&&this._debouncedScan()}_debouncedScan(){var e=this;this._scanTimer&&clearTimeout(this._scanTimer),this._scanTimer=setTimeout(function(){e._scanForErrors()},100)}_parseErrors(e){if(!e)return[];try{var t=JSON.parse(e);return Array.isArray(t)?t:[]}catch{return[]}}_scanForErrors(){var e=[],t=document.querySelectorAll("[has-error], [has-errors]"),r=this;t.forEach(function(n){var i=n.getAttribute("has-error")||n.getAttribute("has-errors"),s=r._parseErrors(i);s.forEach(function(h){e.push({element:n,error:h})})}),this._errors=e,document.dispatchEvent(new CustomEvent("bino-errors-changed",{detail:{count:e.length}})),e.length>0?(this._visible=!0,this._injectBadges(e)):(this._visible=!1,this._removeBadges())}_highlightForElement(e){var t=this,r=this.renderRoot.querySelectorAll("li"),n=null;r.forEach(function(i,s){i.classList.remove("highlighted"),t._errors[s]&&t._errors[s].element===e&&(i.classList.add("highlighted"),n||(n=i))}),n&&n.scrollIntoView({block:"nearest",behavior:"smooth"}),this._highlightTimer&&clearTimeout(this._highlightTimer),this._highlightTimer=setTimeout(function(){r.forEach(function(i){i.classList.remove("highlighted")})},4e3)}_onClose(){this._visible=!1,document.dispatchEvent(new CustomEvent("bino-panel-dismissed"))}_onShowErrors(){this._errors.length>0&&(this._visible=!0)}_scrollToElement(e){e&&(e.scrollIntoView({behavior:"smooth",block:"center"}),e.classList.remove("bn-error-highlight"),e.offsetWidth,e.classList.add("bn-error-highlight"),setTimeout(function(){e.classList.remove("bn-error-highlight")},700))}_injectBadges(e){this._removeBadges();var t=this,r=new Map;e.forEach(function(n,i){r.has(n.element)||r.set(n.element,[]),r.get(n.element).push({error:n.error,index:i})}),r.forEach(function(n,i){var s=document.createElement("div");s.className="bn-error-indicator-badge",s.style.cssText="position:absolute;top:2px;right:2px;width:18px;height:18px;background:#f59e0b;color:#fff;font-size:12px;border-radius:50%;display:flex;align-items:center;justify-content:center;z-index:10000;cursor:pointer;user-select:none;line-height:1;",s.textContent="\u26A0",s.title=n.map(function(m){return m.error.message||m.error.id||"Error"}).join(`
`),s.addEventListener("click",function(m){m.stopPropagation(),t._visible=!0,t._highlightForElement(i)});var h=i.parentNode;if(h){var c=window.getComputedStyle(h);c.position==="static"&&(h.style.position="relative"),i.insertAdjacentElement("afterend",s)}t._badges.push(s)})}_removeBadges(){this._badges.forEach(function(e){e.parentNode&&e.parentNode.removeChild(e)}),this._badges=[]}};customElements.define("bino-error-panel",ye);var xe=class extends O{static properties={_results:{state:!0},_activeIndex:{state:!0},_open:{state:!0}};static styles=M`
    :host {
      position: relative;
      display: inline-block;
      font-family: var(--bino-font-sans);
    }
    .search-wrap {
      position: relative;
      display: flex;
      align-items: center;
    }
    .search-icon {
      position: absolute;
      left: var(--bino-space-sm);
      pointer-events: none;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
      line-height: 1;
    }
    input {
      width: var(--bino-search-width);
      padding: 0.375rem 0.625rem 0.375rem 1.75rem;
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-base);
      font-family: inherit;
      color: var(--bino-text);
      background: #f9fafb;
      transition: width var(--bino-transition-normal), border-color var(--bino-transition-fast), box-shadow var(--bino-transition-fast);
    }
    input:focus {
      width: var(--bino-search-width-focus);
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
      background: var(--bino-surface);
    }
    input::placeholder {
      color: var(--bino-text-secondary);
    }
    .dropdown {
      display: none;
      position: absolute;
      top: 100%;
      right: 0;
      margin-top: 4px;
      min-width: 320px;
      max-height: 400px;
      overflow-y: auto;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border);
      border-radius: var(--bino-radius);
      box-shadow: var(--bino-shadow-dropdown);
      z-index: var(--bino-z-panel);
    }
    .dropdown.open {
      display: block;
    }
    .result {
      display: flex;
      flex-direction: column;
      gap: 2px;
      padding: var(--bino-space-sm) 0.75rem;
      cursor: pointer;
      border-bottom: 1px solid var(--bino-surface-hover);
      font-size: var(--bino-font-size-base);
      transition: background 0.1s;
    }
    .result:last-child {
      border-bottom: none;
    }
    .result:hover, .result.active {
      background: #f0f4ff;
    }
    .result-name {
      font-weight: 500;
      color: var(--bino-text);
    }
    .result-kind {
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      text-transform: uppercase;
      letter-spacing: 0.03em;
    }
    .result-context {
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .no-results {
      padding: 0.75rem;
      text-align: center;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text-secondary);
    }
    mark {
      background: #fef08a;
      color: inherit;
      border-radius: 2px;
      padding: 0 1px;
    }
  `;constructor(){super(),this._results=[],this._activeIndex=-1,this._open=!1,this._debounceTimer=null,this._query=""}connectedCallback(){super.connectedCallback();var e=this;this._boundOutsideClick=function(t){!e.contains(t.target)&&!e.renderRoot.contains(t.target)&&e._close()},document.addEventListener("click",this._boundOutsideClick),this._boundContentUpdated=function(){e._query&&e._search(e._query)},document.addEventListener("bn-preview:content-updated",this._boundContentUpdated)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("click",this._boundOutsideClick),document.removeEventListener("bn-preview:content-updated",this._boundContentUpdated)}render(){var e=this;return b`
      <div class="search-wrap">
        <span class="search-icon">\u2315</span>
        <input type="text" placeholder="Search elements..." autocomplete="off" spellcheck="false"
          @input=${this._onInput}
          @keydown=${this._onKeydown}
          @focus=${this._onFocus}>
      </div>
      <div class="dropdown ${this._open?"open":""}">
        ${this._results.length===0&&this._open?b`<div class="no-results">No results found</div>`:this._results.map(function(t,r){return b`
                <div class="result ${r===e._activeIndex?"active":""}"
                  @click=${()=>e._selectResult(r)}>
                  <span class="result-kind">${t.kind}</span>
                  <span class="result-name">${e._highlightMatch(t,e._query)}</span>
                </div>
              `})}
      </div>
    `}_highlightMatch(e,t){if(!t)return e.name;var r=t.toLowerCase(),n=e.name.toLowerCase().indexOf(r);if(n===-1)return e.name;var i=e.name.substring(0,n),s=e.name.substring(n,n+t.length),h=e.name.substring(n+t.length);return b`${i}<mark>${s}</mark>${h}`}_onInput(e){var t=this,r=e.target.value.trim();clearTimeout(this._debounceTimer),this._debounceTimer=setTimeout(function(){t._query=r,t._search(r)},150)}_onKeydown(e){if(e.key==="Escape"){this._close(),e.target.blur();return}if(e.key==="ArrowDown"){e.preventDefault(),this._moveActive(1);return}if(e.key==="ArrowUp"){e.preventDefault(),this._moveActive(-1);return}if(e.key==="Enter"){e.preventDefault(),this._activeIndex>=0&&this._activeIndex<this._results.length?this._selectResult(this._activeIndex):this._results.length>0&&this._selectResult(0);return}}_onFocus(){this._query&&this._results.length>0&&(this._open=!0)}_search(e){if(this._activeIndex=-1,!e||e.length<2){this._results=[],this._close();return}var t=e.toLowerCase(),r=[],n=new Set,i=document.querySelectorAll("[data-bino-kind]");i.forEach(function(h){var c=h.getAttribute("data-bino-kind")||"",m=h.getAttribute("data-bino-name")||"",$=c+":"+m;n.has($)||(c.toLowerCase().indexOf(t)!==-1||m.toLowerCase().indexOf(t)!==-1)&&(n.add($),r.push({type:"element",kind:c,name:m,el:h}))});var s=document.querySelectorAll("bn-layout-page[data-bino-page]");s.forEach(function(h){var c=h.getAttribute("data-bino-page")||"",m="page:"+c;n.has(m)||c.toLowerCase().indexOf(t)!==-1&&(n.add(m),r.push({type:"page",kind:"LayoutPage",name:c,el:h}))}),r.length<50&&s.forEach(function(h){for(var c=h.getAttribute("data-bino-page")||"",m=h.shadowRoot||h,$=m.querySelectorAll("*"),E=0;E<$.length&&r.length<50;E++){var f=$[E];if(!(f.tagName==="SCRIPT"||f.tagName==="STYLE"))for(var w=f.childNodes,g=0;g<w.length;g++){var A=w[g];if(A.nodeType===3){var k=A.textContent.trim();if(!(!k||k.length<2)){var y=k.toLowerCase().indexOf(t);if(y!==-1){var d=k.substring(Math.max(0,y-30),Math.min(k.length,y+e.length+30)),v="text:"+c+":"+k.substring(y,y+Math.min(40,k.length-y));if(n.has(v))continue;n.add(v),r.push({type:"text",kind:"text in "+c,name:d,el:f,query:e});break}}}}}}),this._results=r,this._open=!0}_moveActive(e){if(this._results.length!==0){var t=this._activeIndex+e;t<0&&(t=this._results.length-1),t>=this._results.length&&(t=0),this._activeIndex=t,this.updateComplete.then(()=>{var r=this.renderRoot.querySelectorAll(".result");r[t]&&r[t].scrollIntoView({block:"nearest"})})}}_selectResult(e){var t=this._results[e];if(!(!t||!t.el)){this._close(),t.el.scrollIntoView({behavior:"smooth",block:"center"});var r=t.el,n=r.style.outline,i=r.style.outlineOffset;r.style.outline="2px solid "+(getComputedStyle(document.documentElement).getPropertyValue("--bino-primary").trim()||"#3b82f6"),r.style.outlineOffset="2px",setTimeout(function(){r.style.outline=n,r.style.outlineOffset=i},3e3)}}_close(){this._open=!1,this._activeIndex=-1}};customElements.define("bino-search",xe);var we=class extends O{static properties={_documents:{state:!0},_open:{state:!0},_selectedDoc:{state:!0},_filterKind:{state:!0}};static styles=M`
    :host {
      font-family: var(--bino-font-sans);
    }
    .backdrop {
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.35);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: 12px;
      box-shadow: 0 20px 60px rgba(0, 0, 0, 0.2);
      width: 640px;
      max-width: calc(100vw - 2rem);
      max-height: 80vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover {
      color: var(--bino-text);
    }
    .kind-tabs {
      display: flex;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-wrap: wrap;
      flex-shrink: 0;
    }
    .kind-tab {
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      cursor: pointer;
      font-family: var(--bino-font-sans);
      user-select: none;
    }
    .kind-tab:hover {
      background: var(--bino-surface-hover);
    }
    .kind-tab.active {
      background: var(--bino-surface-active);
      border-color: var(--bino-primary);
      color: var(--bino-active-text);
      font-weight: 600;
    }
    .doc-list {
      flex: 1;
      overflow-y: auto;
      min-height: 0;
    }
    .doc-row {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      cursor: pointer;
    }
    .doc-row:last-child {
      border-bottom: none;
    }
    .doc-row:hover {
      background: var(--bino-surface-hover);
    }
    .doc-row.selected {
      background: var(--bino-surface-active);
    }
    .kind-badge {
      flex-shrink: 0;
      padding: 2px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      text-transform: uppercase;
      background: #e0e7ff;
      color: #3730a3;
      white-space: nowrap;
    }
    .doc-info {
      min-width: 0;
      flex: 1;
    }
    .doc-name {
      font-weight: 600;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .doc-file {
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .detail-panel {
      border-top: 2px solid var(--bino-border);
      padding: var(--bino-space-md) var(--bino-space-lg);
      flex-shrink: 0;
      background: #f9fafb;
    }
    .detail-row {
      display: flex;
      gap: var(--bino-space-sm);
      margin-bottom: var(--bino-space-xs);
      font-size: var(--bino-font-size-sm);
      align-items: baseline;
    }
    .detail-row:last-child {
      margin-bottom: 0;
    }
    .detail-label {
      color: var(--bino-text-secondary);
      font-weight: 600;
      flex-shrink: 0;
      min-width: 80px;
    }
    .detail-value {
      color: var(--bino-text);
      min-width: 0;
      word-break: break-word;
    }
    .pills {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs);
    }
    .pill {
      padding: 1px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      background: #e5e7eb;
      color: var(--bino-text-muted);
    }
    .pill.label-pill {
      background: #dbeafe;
      color: #1e40af;
    }
    .pill.constraint-pill {
      background: #fef3c7;
      color: #92400e;
    }
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;constructor(){super(),this._documents=[],this._open=!1,this._selectedDoc=null,this._filterKind="",this._boundOnOpen=this._onOpen.bind(this),this._boundOnChanged=this._onChanged.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-assets",this._boundOnOpen),document.addEventListener("bino-documents-changed",this._boundOnChanged),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-assets",this._boundOnOpen),document.removeEventListener("bino-documents-changed",this._boundOnChanged),document.removeEventListener("keydown",this._boundOnKeydown)}render(){if(!this._open)return C;var e=this,t=this._filteredDocs(),r=this._uniqueKinds();return b`
      <div class="backdrop" @click=${this._onBackdropClick}>
        <div class="modal" @click=${this._stopPropagation}>
          <div class="modal-header">
            <h2>Manifest Documents</h2>
            <button class="close-btn" title="Close" @click=${this._close}>&times;</button>
          </div>
          <div class="kind-tabs">
            <button class="kind-tab ${this._filterKind===""?"active":""}"
              @click=${function(){e._filterKind="",e._selectedDoc=null}}>
              All (${this._documents.length})
            </button>
            ${r.map(function(n){var i=e._countKind(n);return b`
                <button class="kind-tab ${e._filterKind===n?"active":""}"
                  @click=${function(){e._filterKind=n,e._selectedDoc=null}}>
                  ${n} (${i})
                </button>
              `})}
          </div>
          <div class="doc-list">
            ${t.length===0?b`<div class="empty">No documents found</div>`:t.map(function(n){var i=e._selectedDoc===n;return b`
                    <div class="doc-row ${i?"selected":""}"
                      @click=${function(){e._selectedDoc=i?null:n}}>
                      <span class="kind-badge">${n.kind}</span>
                      <div class="doc-info">
                        <div class="doc-name">${n.name}</div>
                        <div class="doc-file">${n.file}</div>
                      </div>
                    </div>
                  `})}
          </div>
          ${this._selectedDoc?this._renderDetail(this._selectedDoc):C}
        </div>
      </div>
    `}_renderDetail(e){var t=e.labels||{},r=Object.keys(t),n=e.constraints||[];return b`
      <div class="detail-panel">
        <div class="detail-row">
          <span class="detail-label">Kind</span>
          <span class="detail-value">${e.kind}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">Name</span>
          <span class="detail-value">${e.name}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">File</span>
          <span class="detail-value">${e.file}</span>
        </div>
        ${r.length>0?b`
          <div class="detail-row">
            <span class="detail-label">Labels</span>
            <div class="pills">
              ${r.map(function(i){return b`<span class="pill label-pill">${i}: ${t[i]}</span>`})}
            </div>
          </div>
        `:C}
        ${n.length>0?b`
          <div class="detail-row">
            <span class="detail-label">Constraints</span>
            <div class="pills">
              ${n.map(function(i){return b`<span class="pill constraint-pill">${i}</span>`})}
            </div>
          </div>
        `:C}
      </div>
    `}_filteredDocs(){if(!this._filterKind)return this._documents;var e=this._filterKind;return this._documents.filter(function(t){return t.kind===e})}_uniqueKinds(){var e={},t=[];return this._documents.forEach(function(r){e[r.kind]||(e[r.kind]=!0,t.push(r.kind))}),t}_countKind(e){var t=0;return this._documents.forEach(function(r){r.kind===e&&t++}),t}_onOpen(e){this._documents=e.detail&&e.detail.documents||[],this._open=!0,this._selectedDoc=null,this._filterKind=""}_onChanged(e){if(this._open&&(this._documents=e.detail&&e.detail.documents||[],this._selectedDoc)){var t=this._selectedDoc,r=this._documents.some(function(n){return n.kind===t.kind&&n.name===t.name&&n.file===t.file});r||(this._selectedDoc=null)}}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_onBackdropClick(){this._close()}_stopPropagation(e){e.stopPropagation()}_close(){this._open=!1,this._selectedDoc=null}};customElements.define("bino-assets-modal",we);var q=170,Z=38,$e=20,bt=56,ie=30,vt={ReportArtefact:{bg:"#dbeafe",stroke:"#3b82f6",text:"#1e40af"},DocumentArtefact:{bg:"#dbeafe",stroke:"#3b82f6",text:"#1e40af"},LayoutPage:{bg:"#dcfce7",stroke:"#22c55e",text:"#166534"},LayoutCard:{bg:"#ccfbf1",stroke:"#14b8a6",text:"#115e59"},Component:{bg:"#f3e8ff",stroke:"#a855f7",text:"#6b21a8"},DataSet:{bg:"#ffedd5",stroke:"#f97316",text:"#9a3412"},DataSource:{bg:"#fee2e2",stroke:"#ef4444",text:"#991b1b"},MarkdownFile:{bg:"#f3f4f6",stroke:"#9ca3af",text:"#374151"}},mt={ReportArtefact:"Artefact",DocumentArtefact:"DocArtefact",LayoutPage:"Page",LayoutCard:"Card",Component:"Component",DataSet:"DataSet",DataSource:"Source",MarkdownFile:"Markdown"};function Xe(o){return vt[o]||{bg:"#f3f4f6",stroke:"#9ca3af",text:"#374151"}}function gt(o){return mt[o]||o}function _t(o,e){return e||(e=20),!o||o.length<=e?o||"":o.substring(0,e-1)+"\u2026"}function yt(o){if(!o||!o.rootId)return null;var e=o.nodes||{},t={},r={};function n(i){if(!i)return null;var s=e[i];if(!s)return null;if(t[i])return{id:i,kind:s.kind,name:s.name||i,children:[],cycle:!0};if(r[i])return{id:i,kind:s.kind,name:s.name||i,children:[],ref:!0};t[i]=!0,r[i]=!0;for(var h=[],c=s.dependsOn||[],m=0;m<c.length;m++){var $=n(c[m]);$&&h.push($)}return t[i]=!1,{id:i,kind:s.kind,name:s.name||i,children:h}}return n(o.rootId)}function Ze(o){if(!o)return 0;if(o.children.length===0)return o.w=q,q;for(var e=0,t=0;t<o.children.length;t++)e+=Ze(o.children[t]);return e+=(o.children.length-1)*$e,o.w=Math.max(e,q),o.w}function xt(o){if(!o)return{nodes:[],edges:[],width:0,height:0};Ze(o);var e=[],t=[],r=0,n=0;function i(s,h,c){if(s){var m=h-q/2;if(e.push({id:s.id,kind:s.kind,name:s.name,x:m,y:c,cx:h,ref:s.ref||!1,cycle:s.cycle||!1}),h+q/2>r&&(r=h+q/2),c+Z>n&&(n=c+Z),s.children.length!==0){for(var $=c+Z+bt,E=0,f=0;f<s.children.length;f++)E+=s.children[f].w;E+=(s.children.length-1)*$e;for(var w=h-E/2,f=0;f<s.children.length;f++){var g=s.children[f],A=w+g.w/2;t.push({x1:h,y1:c+Z,x2:A,y2:$}),i(g,A,$),w+=g.w+$e}}}}return i(o,o.w/2+ie,ie),{nodes:e,edges:t,width:r+ie,height:n+ie}}var ke=class extends O{static properties={_graphData:{state:!0},_open:{state:!0}};static styles=M`
    :host { font-family: var(--bino-font-sans); }
    .backdrop {
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.35);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: 12px;
      box-shadow: 0 20px 60px rgba(0, 0, 0, 0.2);
      width: 90vw;
      max-width: 1200px;
      max-height: 85vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover { color: var(--bino-text); }
    .graph-container {
      flex: 1;
      overflow: auto;
      min-height: 0;
      background: #fafbfc;
      padding: var(--bino-space-md);
    }
    svg { display: block; }
    svg text { font-family: var(--bino-font-sans); }
    .legend {
      display: flex;
      gap: var(--bino-space-md);
      flex-wrap: wrap;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .legend-item {
      display: flex;
      align-items: center;
      gap: var(--bino-space-xs);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
    }
    .legend-swatch {
      width: 12px;
      height: 12px;
      border-radius: 3px;
    }
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;constructor(){super(),this._graphData=null,this._open=!1,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-graph",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-graph",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown)}render(){if(!this._open)return C;var e=yt(this._graphData),t=xt(e);return b`
      <div class='backdrop' @click=${this._close}>
        <div class='modal' @click=${this._stop}>
          <div class='modal-header'>
            <h2>Dependency Graph</h2>
            <button class='close-btn' title='Close' @click=${this._close}>&times;</button>
          </div>
          <div class='graph-container'>
            ${this._renderSVG(t)}
          </div>
          ${this._renderLegend(t)}
        </div>
      </div>
    `}_renderSVG(e){if(!e||e.nodes.length===0)return b`<div class='empty'>No graph data available</div>`;var t=e.width,r=e.height,n=e.edges.map(function(s){var h=(s.y1+s.y2)/2;return me`<path d=${"M"+s.x1+","+s.y1+" C"+s.x1+","+h+" "+s.x2+","+h+" "+s.x2+","+s.y2}
        fill='none' stroke='#cbd5e1' stroke-width='1.5'/>`}),i=e.nodes.map(function(s){var h=Xe(s.kind),c=s.ref||s.cycle?"0.5":"1",m=gt(s.kind),$=_t(s.name),E=s.cycle?" [cycle]":s.ref?" [ref]":"";return me`
        <g opacity=${c}>
          <rect x=${s.x} y=${s.y} width=${q} height=${Z}
            rx='6' fill=${h.bg} stroke=${h.stroke} stroke-width='1.5'/>
          <text x=${s.cx} y=${s.y+14} text-anchor='middle'
            font-size='10' font-weight='600' fill=${h.stroke}>${m}</text>
          <text x=${s.cx} y=${s.y+28} text-anchor='middle'
            font-size='11' fill=${h.text}>${$}${E}</text>
        </g>
      `});return b`
      <svg width=${t} height=${r} viewBox=${"0 0 "+t+" "+r}>
        ${n}
        ${i}
      </svg>
    `}_renderLegend(e){if(!e||e.nodes.length===0)return C;var t={},r=[];return e.nodes.forEach(function(n){t[n.kind]||(t[n.kind]=!0,r.push(n.kind))}),r.length===0?C:b`
      <div class='legend'>
        ${r.map(function(n){var i=Xe(n);return b`
            <span class='legend-item'>
              <span class='legend-swatch' style=${"background:"+i.bg+";border:1.5px solid "+i.stroke}></span>
              ${n}
            </span>
          `})}
      </div>
    `}_onOpen(e){this._graphData=e.detail&&e.detail.graph||null,this._open=!0}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_close(){this._open=!1}_stop(e){e.stopPropagation()}};customElements.define("bino-graph-modal",ke);var wt=[25,50,100,250],Ee=class extends O{static properties={_open:{state:!0},_metadata:{state:!0},_sql:{state:!0},_result:{state:!0},_summarizeResult:{state:!0},_loading:{state:!0},_error:{state:!0},_page:{state:!0},_pageSize:{state:!0},_activeTab:{state:!0},_expandedSource:{state:!0},_refreshing:{state:!0}};static styles=M`
    :host {
      font-family: var(--bino-font-sans);
    }
    .backdrop {
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.5);
      z-index: var(--bino-z-modal);
      display: flex;
      flex-direction: column;
    }
    .explorer {
      display: flex;
      flex-direction: column;
      width: 100%;
      height: 100%;
      background: var(--bino-surface);
    }
    .explorer-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
      background: var(--bino-surface);
    }
    .explorer-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .header-actions {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
    }
    .refresh-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: 4px 10px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      color: var(--bino-text-secondary);
    }
    .refresh-btn:hover {
      background: var(--bino-surface-hover);
      border-color: #9ca3af;
      color: var(--bino-text);
    }
    .refresh-btn.refreshing {
      opacity: 0.6;
      cursor: not-allowed;
    }
    .refresh-icon {
      display: inline-block;
      font-size: var(--bino-font-size-md);
      line-height: 1;
    }
    .refresh-btn.refreshing .refresh-icon {
      animation: spin 0.8s linear infinite;
    }
    @keyframes spin {
      to { transform: rotate(360deg); }
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover {
      color: var(--bino-text);
    }
    .explorer-body {
      display: flex;
      flex: 1;
      min-height: 0;
      overflow: hidden;
    }
    .sidebar {
      width: 280px;
      min-width: 280px;
      border-right: 1px solid var(--bino-border);
      overflow-y: auto;
      background: #fafbfc;
      flex-shrink: 0;
    }
    .sidebar-section {
      padding: var(--bino-space-sm) 0;
    }
    .sidebar-title {
      padding: var(--bino-space-xs) var(--bino-space-md);
      font-size: var(--bino-font-size-xs);
      font-weight: 700;
      text-transform: uppercase;
      color: var(--bino-text-secondary);
      letter-spacing: 0.05em;
    }
    .sidebar-item {
      display: flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: 5px var(--bino-space-md);
      cursor: pointer;
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
      border-left: 3px solid transparent;
    }
    .sidebar-item:hover {
      background: var(--bino-surface-hover);
      border-left-color: var(--bino-border-light);
    }
    .sidebar-item-name {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-weight: 500;
    }
    .sidebar-item-badge {
      flex-shrink: 0;
      padding: 1px 5px;
      border-radius: 3px;
      font-size: 10px;
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge-source {
      background: #fee2e2;
      color: #991b1b;
    }
    .badge-dataset {
      background: #ffedd5;
      color: #9a3412;
    }
    .sidebar-info-btn {
      flex-shrink: 0;
      background: none;
      border: none;
      cursor: pointer;
      color: var(--bino-text-secondary);
      font-size: 14px;
      padding: 0 2px;
      line-height: 1;
    }
    .sidebar-info-btn:hover {
      color: var(--bino-primary);
    }
    .column-list {
      padding: 2px var(--bino-space-md) var(--bino-space-xs) calc(var(--bino-space-md) + 12px);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
    }
    .column-entry {
      display: flex;
      justify-content: space-between;
      padding: 1px 0;
    }
    .column-name {
      font-weight: 500;
      color: var(--bino-text-muted);
    }
    .column-type {
      font-style: italic;
      color: var(--bino-text-secondary);
    }
    .main-panel {
      flex: 1;
      display: flex;
      flex-direction: column;
      min-width: 0;
      overflow: hidden;
    }
    .editor-area {
      display: flex;
      flex-direction: column;
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .sql-editor {
      width: 100%;
      min-height: 100px;
      max-height: 200px;
      padding: var(--bino-space-sm) var(--bino-space-md);
      border: none;
      outline: none;
      resize: vertical;
      font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
      font-size: var(--bino-font-size-sm);
      line-height: 1.5;
      background: #1e1e2e;
      color: #cdd6f4;
      box-sizing: border-box;
    }
    .sql-editor::placeholder {
      color: #6c7086;
    }
    .editor-toolbar {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      background: #f1f3f5;
      border-top: 1px solid var(--bino-border);
    }
    .editor-btn {
      padding: 4px 12px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      color: var(--bino-text-muted);
    }
    .editor-btn:hover {
      background: var(--bino-surface-hover);
      border-color: #9ca3af;
    }
    .editor-btn.primary {
      background: var(--bino-primary);
      border-color: var(--bino-primary);
      color: #fff;
    }
    .editor-btn.primary:hover {
      background: var(--bino-primary-hover);
    }
    .editor-btn:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .editor-shortcut {
      font-size: 10px;
      color: var(--bino-text-secondary);
      margin-left: auto;
    }
    .results-area {
      flex: 1;
      display: flex;
      flex-direction: column;
      min-height: 0;
      overflow: hidden;
    }
    .tab-bar {
      display: flex;
      gap: 0;
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
      background: #f9fafb;
    }
    .tab-btn {
      padding: var(--bino-space-xs) var(--bino-space-md);
      border: none;
      background: none;
      font-size: var(--bino-font-size-sm);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-secondary);
      cursor: pointer;
      border-bottom: 2px solid transparent;
      font-weight: 500;
    }
    .tab-btn:hover {
      color: var(--bino-text);
      background: var(--bino-surface-hover);
    }
    .tab-btn.active {
      color: var(--bino-primary);
      border-bottom-color: var(--bino-primary);
      font-weight: 600;
    }
    .table-container {
      flex: 1;
      overflow: auto;
      min-height: 0;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: var(--bino-font-size-sm);
    }
    thead {
      position: sticky;
      top: 0;
      z-index: 1;
    }
    th {
      background: #f1f3f5;
      padding: 6px 12px;
      text-align: left;
      font-weight: 600;
      color: var(--bino-text-muted);
      border-bottom: 2px solid var(--bino-border);
      white-space: nowrap;
      font-size: var(--bino-font-size-xs);
    }
    .col-type {
      font-weight: 400;
      color: var(--bino-text-secondary);
      font-style: italic;
      margin-left: 4px;
    }
    td {
      padding: 4px 12px;
      border-bottom: 1px solid var(--bino-border);
      max-width: 300px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      color: var(--bino-text);
    }
    tr:nth-child(even) {
      background: #fafbfc;
    }
    tr:hover {
      background: var(--bino-surface-hover);
    }
    .pagination {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      background: #f9fafb;
    }
    .pagination button {
      padding: 2px 8px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      cursor: pointer;
      font-size: var(--bino-font-size-xs);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-muted);
    }
    .pagination button:hover:not(:disabled) {
      background: var(--bino-surface-hover);
    }
    .pagination button:disabled {
      opacity: 0.4;
      cursor: not-allowed;
    }
    .pagination select {
      padding: 2px 4px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      font-size: var(--bino-font-size-xs);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-muted);
      background: var(--bino-surface);
    }
    .status-bar {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      border-top: 1px solid var(--bino-border);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      background: #f9fafb;
      flex-shrink: 0;
    }
    .error-msg {
      padding: var(--bino-space-md);
      color: var(--bino-error);
      font-size: var(--bino-font-size-sm);
      white-space: pre-wrap;
      word-break: break-word;
    }
    .loading-msg {
      padding: var(--bino-space-lg);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
    }
    .empty-state {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
    .duration {
      color: var(--bino-text-secondary);
    }
  `;constructor(){super(),this._open=!1,this._metadata=null,this._sql="",this._result=null,this._summarizeResult=null,this._loading=!1,this._error="",this._page=0,this._pageSize=50,this._activeTab="results",this._expandedSource=null,this._refreshing=!1,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this),this._boundOnDocsChanged=this._onDocsChanged.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-explorer",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown),document.addEventListener("bino-documents-changed",this._boundOnDocsChanged)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-explorer",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown),document.removeEventListener("bino-documents-changed",this._boundOnDocsChanged)}render(){if(!this._open)return C;var e=this;return b`
      <div class="backdrop">
        <div class="explorer">
          <div class="explorer-header">
            <h2>Data Explorer</h2>
            <div class="header-actions">
              <button class="refresh-btn ${this._refreshing?"refreshing":""}"
                title="Refresh data sources and datasets"
                ?disabled=${this._refreshing}
                @click=${this._onRefresh}>
                <span class="refresh-icon">\u21BB</span>
                <span>${this._refreshing?"Refreshing...":"Refresh"}</span>
              </button>
              <button class="close-btn" title="Close (Esc)" @click=${this._close}>&times;</button>
            </div>
          </div>
          <div class="explorer-body">
            ${this._renderSidebar()}
            ${this._renderMainPanel()}
          </div>
        </div>
      </div>
    `}_renderSidebar(){var e=this,t=this._metadata;return t?b`
      <div class="sidebar">
        ${t.sources&&t.sources.length>0?b`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSources (${t.sources.length})</div>
            ${t.sources.map(function(r){var n=e._expandedSource===r.name;return b`
                <div>
                  <div class="sidebar-item" @click=${function(){e._selectSource(r.name)}}>
                    <span class="sidebar-item-name">${r.name}</span>
                    ${r.type?b`<span class="sidebar-item-badge badge-source">${r.type}</span>`:C}
                    <button class="sidebar-info-btn" title="Show columns"
                      @click=${function(i){i.stopPropagation(),e._toggleSourceInfo(r.name)}}>
                      ${n?"\u25B4":"\u25BE"}
                    </button>
                  </div>
                  ${n&&r.columns&&r.columns.length>0?b`
                    <div class="column-list">
                      ${r.columns.map(function(i){return b`
                          <div class="column-entry">
                            <span class="column-name">${i.name}</span>
                            <span class="column-type">${i.type}</span>
                          </div>
                        `})}
                    </div>
                  `:C}
                </div>
              `})}
          </div>
        `:C}
        ${t.datasets&&t.datasets.length>0?b`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSets (${t.datasets.length})</div>
            ${t.datasets.map(function(r){return b`
                <div class="sidebar-item" @click=${function(){e._selectDataset(r.name)}}>
                  <span class="sidebar-item-name">${r.name}</span>
                  <span class="sidebar-item-badge badge-dataset">set</span>
                </div>
              `})}
          </div>
        `:C}
        ${(!t.sources||t.sources.length===0)&&(!t.datasets||t.datasets.length===0)?b`<div class="empty-state">No data sources found</div>`:C}
      </div>
    `:b`<div class="sidebar"><div class="loading-msg">Loading...</div></div>`}_renderMainPanel(){var e=this;return b`
      <div class="main-panel">
        <div class="editor-area">
          <textarea class="sql-editor"
            placeholder="Enter SQL query... (Ctrl+Enter to run)"
            .value=${this._sql}
            @input=${function(t){e._sql=t.target.value}}
            @keydown=${this._onEditorKeydown.bind(this)}
            spellcheck="false"
          ></textarea>
          <div class="editor-toolbar">
            <button class="editor-btn primary" ?disabled=${this._loading} @click=${this._runQuery.bind(this)}>Run</button>
            <button class="editor-btn" ?disabled=${this._loading||!this._sql.trim()} @click=${this._runSummarize.bind(this)}>Summarize</button>
            <button class="editor-btn" @click=${this._clearEditor.bind(this)}>Clear</button>
            <span class="editor-shortcut">${navigator.platform.includes("Mac")?"\u2318":"Ctrl"}+Enter to run</span>
          </div>
        </div>
        <div class="results-area">
          <div class="tab-bar">
            <button class="tab-btn ${this._activeTab==="results"?"active":""}"
              @click=${function(){e._activeTab="results"}}>Results</button>
            <button class="tab-btn ${this._activeTab==="summarize"?"active":""}"
              @click=${function(){e._activeTab="summarize"}}>Summarize</button>
          </div>
          ${this._activeTab==="results"?this._renderResults():this._renderSummarizeTab()}
        </div>
      </div>
    `}_renderResults(){if(this._loading)return b`<div class="loading-msg">Executing query...</div>`;if(this._error)return b`<div class="error-msg">${this._error}</div>`;if(!this._result)return b`<div class="empty-state">Run a query to see results</div>`;if(this._result.error)return b`
        <div class="error-msg">${this._result.error}</div>
        ${this._result.durationMs!=null?b`<div class="status-bar"><span class="duration">${this._result.durationMs}ms</span></div>`:C}
      `;var e=this,t=this._result.columns||[],r=this._result.rows||[],n=this._result.totalRows,i=typeof n=="number"?Math.ceil(n/this._pageSize):null;return b`
      <div class="table-container">
        ${t.length===0?b`<div class="empty-state">No columns returned</div>`:b`
            <table>
              <thead>
                <tr>
                  ${t.map(function(s){return b`<th>${s.name}<span class="col-type">${s.type}</span></th>`})}
                </tr>
              </thead>
              <tbody>
                ${r.map(function(s){return b`<tr>${s.map(function(h){return b`<td title="${h!=null?String(h):""}">${h!=null?String(h):""}</td>`})}</tr>`})}
              </tbody>
            </table>
          `}
      </div>
      <div class="pagination">
        <button ?disabled=${this._page===0} @click=${function(){e._page--,e._rerunQuery()}}>\u2190</button>
        <span>Page ${this._page+1}${i!=null?" of "+i:""}</span>
        <button ?disabled=${i!=null&&this._page>=i-1} @click=${function(){e._page++,e._rerunQuery()}}>\u2192</button>
        <span>|</span>
        <select .value=${String(this._pageSize)} @change=${function(s){e._pageSize=parseInt(s.target.value,10),e._page=0,e._rerunQuery()}}>
          ${wt.map(function(s){return b`<option value=${s} ?selected=${s===e._pageSize}>${s} rows</option>`})}
        </select>
        <span class="duration">${this._result.durationMs!=null?this._result.durationMs+"ms":""}</span>
        ${typeof n=="number"?b`<span>${n} total rows</span>`:C}
      </div>
    `}_renderSummarizeTab(){if(this._loading)return b`<div class="loading-msg">Running SUMMARIZE...</div>`;if(!this._summarizeResult)return b`<div class="empty-state">Click "Summarize" to see column statistics</div>`;if(this._summarizeResult.error)return b`<div class="error-msg">${this._summarizeResult.error}</div>`;var e=this._summarizeResult.columns||[],t=this._summarizeResult.rows||[];return b`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              ${e.map(function(r){return b`<th>${r.name}<span class="col-type">${r.type}</span></th>`})}
            </tr>
          </thead>
          <tbody>
            ${t.map(function(r){return b`<tr>${r.map(function(n){return b`<td title="${n!=null?String(n):""}">${n!=null?String(n):""}</td>`})}</tr>`})}
          </tbody>
        </table>
      </div>
      <div class="status-bar">
        <span class="duration">${this._summarizeResult.durationMs!=null?this._summarizeResult.durationMs+"ms":""}</span>
      </div>
    `}_onOpen(){this._open=!0,this._fetchMetadata()}_close(){this._open=!1}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_onDocsChanged(){this._open&&this._fetchMetadata()}_onRefresh(){if(!this._refreshing){this._refreshing=!0;var e=this;this._fetchMetadata(function(){e._refreshing=!1})}}_onEditorKeydown(e){if(e.key==="Tab"){e.preventDefault();var t=e.target,r=t.selectionStart,n=t.selectionEnd;this._sql=this._sql.substring(0,r)+"  "+this._sql.substring(n),this.updateComplete.then(function(){t.selectionStart=t.selectionEnd=r+2});return}e.key==="Enter"&&(e.metaKey||e.ctrlKey)&&(e.preventDefault(),this._runQuery())}_selectSource(e){this._sql='SELECT * FROM "'+e+'"',this._page=0,this._activeTab="results",this._runQuery()}_selectDataset(e){this._sql='SELECT * FROM "'+e+'"',this._page=0,this._activeTab="results",this._runQuery()}_toggleSourceInfo(e){this._expandedSource=this._expandedSource===e?null:e}_clearEditor(){this._sql="",this._result=null,this._summarizeResult=null,this._error="",this._page=0}_rerunQuery(){this._sql.trim()&&this._runQuery()}_runQuery(){var e=this._sql.trim();if(e){var t=this;this._loading=!0,this._error="",this._activeTab="results",fetch("/__explorer/query",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e,limit:this._pageSize,offset:this._page*this._pageSize})}).then(function(r){return r.json()}).then(function(r){t._result=r,t._loading=!1}).catch(function(r){t._error=r.message||"Query failed",t._loading=!1})}}_runSummarize(){var e=this._sql.trim();if(e){var t=this;this._loading=!0,this._error="",this._activeTab="summarize",fetch("/__explorer/summarize",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e})}).then(function(r){return r.json()}).then(function(r){t._summarizeResult=r,t._loading=!1}).catch(function(r){t._error=r.message||"Summarize failed",t._loading=!1})}}_fetchMetadata(e){var t=this;fetch("/__explorer/metadata").then(function(r){return r.json()}).then(function(r){t._metadata=r,e&&e()}).catch(function(r){console.error("explorer: fetch metadata failed",r),e&&e()})}};customElements.define("bino-data-explorer",Ee);if(!(!window.EventSource||window.__bnPreviewRuntime)){let o=function(){Ae&&Se&&e("initial")},e=function(r){var n=++Ce;fetch("/__preview/context?path="+encodeURIComponent(N)).then(function(i){return i.ok?i.text():(console.warn("bn preview: context fetch failed",i.status,r),null)}).then(function(i){if(!(!i||n!==Ce)){var s=Te(i,et);if(s){ze=!0;try{document.dispatchEvent(new CustomEvent("bn-preview:content-updated",{detail:{path:N}}))}catch(h){console.debug("bn preview: custom event skipped",h)}}else console.warn("bn preview: swapContext returned false; DOM not updated")}}).catch(function(i){console.error("bn preview: context fetch errored",i)})},t=function(){if(N==="/"){document.querySelectorAll(".bn-page-info").forEach(function(m){m.remove()});var r=document.querySelector("bn-context");if(r){var n=r.getAttribute("data-page-meta");if(n){var i;try{i=JSON.parse(n)}catch{return}if(Array.isArray(i)){var s={};i.forEach(function(m){s[m.name]=m});var h=r.shadowRoot||r,c=h.querySelectorAll("bn-layout-page[data-bino-page]");c.length===0&&(c=document.querySelectorAll("bn-layout-page[data-bino-page]")),c.forEach(function(m){var $=m.getAttribute("data-bino-page");if($){var E=$.split("#")[0],f=s[E]||s[$];if(f){var w=document.createElement("div");w.className="bn-page-info";var g=document.createElement("span");if(g.className="bn-page-info-name",g.textContent=$,w.appendChild(g),f.constraints&&f.constraints.length>0){var A=document.createElement("span");A.className="bn-page-info-label",A.textContent="constraints:",w.appendChild(A),f.constraints.forEach(function(y){var d=document.createElement("span");d.className="bn-page-info-pill constraint",d.textContent=y,w.appendChild(d)})}if(f.artifacts&&f.artifacts.length>0){var k=document.createElement("span");k.className="bn-page-info-label",k.textContent="used in:",w.appendChild(k),f.artifacts.forEach(function(y){var d=document.createElement("span");d.className="bn-page-info-pill artefact",d.textContent=y,w.appendChild(d)})}m.parentNode.insertBefore(w,m)}}})}}}}};window.__bnPreviewRuntime=!0,console.info("bn preview runtime v10 (bundled idiomorph + lit)"),et=new DOMParser,N=j(window.location.pathname||"/"),B=new EventSource("/__preview/events"),Ae=!1,Se=!1,Le().then(function(){Se=!0,o()}),B.addEventListener("ready",function(){Ae=!0,document.dispatchEvent(new CustomEvent("bn-preview:refresh-done")),o()}),B.addEventListener("refreshing",function(r){try{var n=JSON.parse(r.data||"{}");document.dispatchEvent(new CustomEvent("bn-preview:refreshing",{detail:n}))}catch{document.dispatchEvent(new CustomEvent("bn-preview:refreshing",{detail:{}}))}}),B.addEventListener("refresh-done",function(r){var n={};try{n=JSON.parse(r.data||"{}")||{}}catch{n={}}var i=Array.isArray(n.paths)?n.paths:[],s=!1;if(i.length>0){for(var h=0;h<i.length;h++)if(j(i[h])===N){s=!0;break}s||(console.warn("bn preview: refresh did not include this view",N,"broadcast paths:",i),document.dispatchEvent(new CustomEvent("bn-preview:no-payload",{detail:{path:N,broadcastPaths:i}})))}s&&!ze&&e("initial-retry"),document.dispatchEvent(new CustomEvent("bn-preview:refresh-done",{detail:n}))}),Ce=0,ze=!1,B.addEventListener("path-changed",function(r){var n={};try{n=JSON.parse(r.data||"{}")||{}}catch{return}!n.path||j(n.path)!==N||e("path-changed")}),B.addEventListener("refresh-error",function(r){var n={};try{n=JSON.parse(r.data||"{}")}catch{}n&&n.path&&j(n.path)!==N||(console.error("bn preview: refresh failed",n&&n.message),document.dispatchEvent(new CustomEvent("bn-preview:refresh-error",{detail:n})))}),window.addEventListener("beforeunload",function(){B.close()}),document.addEventListener("click",function(r){if(!(!r.metaKey&&!r.ctrlKey)){var n=r.target.closest("[data-bino-kind]");if(n){var i={type:"bino:revealSource",kind:n.getAttribute("data-bino-kind"),name:n.getAttribute("data-bino-name")||"",ref:n.getAttribute("data-bino-ref")||""};window.parent&&window.parent!==window&&window.parent.postMessage(i,"*"),r.preventDefault(),r.stopPropagation()}}}),document.addEventListener("bn-preview:content-updated",function(){t()}),document.readyState==="loading"?document.addEventListener("DOMContentLoaded",t):t()}var et,N,B,Ae,Se,Ce,ze;
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
