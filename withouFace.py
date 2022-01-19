import sys
import subprocess
from py2graphql import Client
from mininet.net import Containernet
from mininet.node import Controller, Switch
from mininet.cli import CLI
from mininet.link import TCLink
from mininet.log import info, setLogLevel
import os

setLogLevel('info')

net = Containernet(controller=Controller)


info('*** Configuring hugepages\n')
subprocess.run('while ! dpdk-hugepages.py --setup 16G; do sleep 1; done',
               shell=True, check=True, stdout=sys.stdout, stderr=sys.stderr)

info('*** Adding controller\n')
net.addController('c0')

info('*** Configuring Docker volume\n')
subprocess.run('docker volume create run-ndn', shell=True,
               check=True, stdout=sys.stdout, stderr=sys.stderr)

#sFlow integration for real time network data analysis
#exec(open("./sflow-rt/extras/sflow.py").read())
class fwctrl: #forwarder class

    def __init__(self, name, ip ,net = net): # construct GraphQL client
        self.name= name
        self.ip= ip
        self.faces= {}

        NDN_DPDK_KWARGS = dict(
        dimage='ndn-dpdk',
        dcmd='/usr/local/bin/ndndpdk-svc --gqlserver http://:3030',
        volumes=['run-ndn:/run/ndn'],
        cap_add=['IPC_LOCK', 'NET_ADMIN', 'SYS_ADMIN', 'SYS_NICE']
        )
        
        info('*** Adding docker containers\n')
        self.host = net.addDocker( name , ip=ip, **NDN_DPDK_KWARGS)
        self.links = [] #list of our links

    def createLink(self, other,localMAC, remoteMAC, net=net):
        info('*** First host is '+ str(self.host) +'\n')
        info('*** Second host is '+ str(other.host)+'\n')
        link = net.addLink(self.host, other.host, addr1=localMAC, addr2=remoteMAC)

        self.links.append(
            link
        ) #adding link to the lists created above

        other.links.append(
            link
        )

    def start_network(self):

        for link in self.links:
            for intf in [link.intf1, link.intf2]:
                intf.cmd(['ip', 'link', 'set', intf.name, 'up'])

                self.graphQLClient = Client(url='http://%s:3030' %
                    self.host.dcinfo['NetworkSettings']['IPAddress'], headers={})

        info('*** Activating NDN-DPDK\n')
        for client in [self.graphQLClient]:
            result = client.fetch("""
                mutation activate($arg: JSON!) {
                    activate(forwarder: $arg)
                }
            """, {
                'arg': {
                    'eal': {
                        'lcoresPerNuma': {'0': 6},
                        'memPerNuma': {'0': 4096}
                    },
                    'mempool': {
                        'DIRECT': {'capacity': 65535, 'dataroom': 9128},
                        'INDIRECT': {'capacity': 65535},
                        'PAYLOAD': {'capacity': 65535, 'dataroom': 9128}
                    }
                }
            })
            info('activate result %s\n' % result)

    def createEtherFace(self,  localMAC, remoteMAC, name):  # returns face ID
        info('*** Creating face\n')
        face = self.graphQLClient.fetch("""
            mutation createFace($locator: JSON!) {
                createFace(locator: $locator) {
                    id
                }
            }
        """, {
            'locator': {
                'scheme': 'ether',
                'local': localMAC ,
                'remote': remoteMAC,
                'vdevConfig': {
                'xdp': {
                    'disabled': True
                }
            }
            }
        }
        )
        self.faces[name] = face
        info('***face result %s\n' % face)

    def loadStrategy(self):
        info('Loading strategy......')

        strategy = self.graphQLClient.fetch("""
                mutation loadStrategy($name: String!, $elf: Bytes!) {
                    loadStrategy(name: $name, elf: $elf) {
                            id
                            name
                    }
                } 
                
                """, {
                    "elf": "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",
                     "name": "ndndpdk-strategy-sequential.o"
                })
        strategyID= strategy['data']['loadStrategy']['id']        
        info('***strategy result %s\n' % strategy)
        info('id is %s \n ' %strategyID)
        
        return strategyID    
        
    def insertFibEntry(self, prefix, names): # returns FIB entry ID
        ids = [ self.faces[name]['data']['createFace']['id'] for name in names]

        fib = self.graphQLClient.fetch("""
            mutation insertFibEntry($name: Name!, $nexthops: [ID!]!) {
                insertFibEntry(name: $name, nexthops: $nexthops) {
                    id
                }
            }
        """, {
            'name': "/"+prefix ,
            'nexthops': ids
        })
        info('***fib result %s\n' % fib)
        info('***prefix is %s'% "'/"+prefix+"'"+ '\n')
        fib_id = fib['data']['insertFibEntry']['id']
        print(fib_id)
        return fib_id #this is returning fib id which we can store in a dictionary or list for further actions

    def insertFibEntry_strategy(self, prefix, names,strategyID): # returns FIB entry ID
        ids = [ self.faces[name]['data']['createFace']['id'] for name in names]
         
        fib = self.graphQLClient.fetch("""
            mutation insertFibEntry($name: Name!, $nexthops: [ID!]!, $strategy: ID) {
                insertFibEntry(name: $name, nexthops: $nexthops, strategy: $strategy) {
                    id
                }
            }
        """, {
            'name': "/"+prefix ,
            'nexthops': ids ,
            'strategy' : strategyID
        })
        info('***fib result %s\n' % fib)
        info('***prefix is %s'% "'/"+prefix+"'"+ '\n')
        fib_id = fib['data']['insertFibEntry']['id']
        print(fib_id)
        return fib_id #this is returning fib id which we can store in a dictionary or list for further actions



#function to delete FIB

    def deleteObj(self,id): # returns FIB deletion boolean
        
        objDel = self.graphQLClient.fetch("""
            mutation delete($id:ID!) {
                delete(id: $id)
            }
        """, {
            'id': id 
            
        })
        info('***fib deletion result %s\n' % objDel)
       

    def start_producer(self,name):
        info('*** Starting producer\n')
        self.name= name
        producer="ndndpdk-godemo pingserver --name /"+self.name
        self.host.popen(producer , shell=True)
        info(producer)


    


                                            

    def get_rxData(self):
        info ('\n getting rxData...\n ')
        rxData = self.graphQLClient.fetch("""
            query faces {
                faces{
                    counters
                    {
                        rxData
                    }
                }
            }
        """, )
        info('***rxData for face is %s\n' % rxData)
    
    
    def get_face(self):
        info ('\n getting faces... \n')
        facesID = self.graphQLClient.fetch("""
            query faces {
                faces{
                    id
                }
            }
        """, )
        info('***facesID for host are %s\n' % facesID)
    



    def roundRobin():
       
        info('\n Input 1 to start the producer and consumer. \n')
        input1= input()
        print(input1)
        #here we write the code for path discovery or easier way to say more than one path to go.
        #if first and third alphabets are same that means more than 1 path
        for key in fib_ids:
            #print(key)
            #print (key[0],key[2])
            matches = 0
            for key2 in fib_ids:
                if key[0]== key2[0] and key[2]== key2[2]:
                    matches += 1
            
            if matches > 1:
                print(key)

    def triggeredRoundRobin():
        info('this is triggered roundRobin')

    def weightedRoundRobin():
        info('this is weighted roundRobin')

def loadbalancer():
    info('\n *****Select load balancing technique***** \n')
    info('1. Round Robin \n')
    info('2. Weighted Round Robin \n')
    info('3. Triggered Round Robin \n')
    input1= input()
    if input1==1:
        P.roundRobin()
    elif input1==2:
        P.weightedRoundRobin()
    elif input1==3:
        P.triggeredRoundRobin()        

#call init to activate containernet hosts and graphQL client 
P = fwctrl('P', '10.58.13.1/24')
Q = fwctrl('Q', '10.58.13.2/24')
R = fwctrl('R', '10.58.13.3/24')
S = fwctrl('S', '10.58.13.4/24')
T = fwctrl('T', '10.58.13.5/24')
U = fwctrl('U', '10.58.13.6/24')
V = fwctrl('V', '10.58.13.7/24')
Z = fwctrl('Z', '10.58.13.8/24')


P.createLink(Q,'02:60:18:6d:b0:01','02:60:18:6d:b0:02') #P to Q link   
Q.createLink(R,'02:60:18:6d:b0:03','02:60:18:6d:b0:04') #Q to R link 
Q.createLink(S,'02:60:18:6d:b0:05','02:60:18:6d:b0:06') #Q to S link 
Q.createLink(T,'02:60:18:6d:b0:07','02:60:18:6d:b0:08') #Q to T link 
#S.createLink(U,'02:60:18:6d:b0:09','02:60:18:6d:b0:10') #S to U link
T.createLink(U,'02:60:18:6d:b0:11','02:60:18:6d:b0:12') #T to U link  
U.createLink(V,'02:60:18:6d:b0:13','02:60:18:6d:b0:14') #U to V link
S.createLink(Z,'02:60:18:6d:b0:15','02:60:18:6d:b0:16') #S to Z link
Z.createLink(U,'02:60:18:6d:b0:17','02:60:18:6d:b0:18') #Z to U link

P.links

info('*** Starting network\n')
net.start()

#establishes link and activate ndn-dpdk forwarders
P.start_network()
Q.start_network()
R.start_network()
S.start_network()
T.start_network()
U.start_network()
V.start_network()
Z.start_network()

#strategyID=Q.loadStrategy()

#call createEtherFace to make faces

#Direct FIBs
fib_ids = {} #for storing fib ids in a dictionary eg QPQ is key for fibentry Q on facePQ




#start producer
P.start_producer('P')
Q.start_producer('Q')
R.start_producer('R')
S.start_producer('S')
Z.start_producer('Z')
T.start_producer('T')
U.start_producer('U')
V.start_producer('V')

#delete fibEntry
#Z.deleteObj(fib_ids['V_ZU'])

#calling updateFaceCounter




#print('main strategy is', strategyID)
info("""*** Running CLI
Try:
    P ndndpdk-godemo pingclient --name /V
    P ndndpdk-godemo pingclient --name /Q
    P ndndpdk-godemo pingclient --name /R
    Q ndndpdk-godemo pingclient --name /P
    Q ndndpdk-godemo pingclient --name /R
    R ndndpdk-godemo pingclient --name /Q
    Q ndndpdk-godemo pingclient --name /S
    S ndndpdk-godemo pingclient --name /Q
    T ndndpdk-godemo pingclient --name /Q
    S ndndpdk-godemo pingclient --name /U
    T ndndpdk-godemo pingclient --name /U
    U ndndpdk-godemo pingclient --name /V
""")
#loadbalancer()

#os.system('Q ndndpdk-ctrl load-strategy --elffile /usr/local/lib/bpf/ndndpdk-strategy-sequential.o')
CLI(net)


info('*** Stopping network\n')
net.stop()

